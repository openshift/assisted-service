package generator

import (
	"context"
	"os"
	"path/filepath"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/ignition"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/internal/provider/registry"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen --build_flags=--mod=mod -package generator -destination mock_install_config.go . InstallConfigGenerator
type InstallConfigGenerator interface {
	GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte, releaseImage, installerReleaseImageOverride string) error
}

type Config struct {
	ServiceCACertPath      string `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	ReleaseImageMirror     string `envconfig:"OPENSHIFT_INSTALL_RELEASE_IMAGE_MIRROR" default:""`
	DummyIgnition          bool   `envconfig:"DUMMY_IGNITION"`
	InstallInvoker         string `envconfig:"INSTALL_INVOKER" default:"assisted-installer"`
	InstallerCacheCapacity int64  `envconfig:"INSTALLER_CACHE_CAPACITY"`

	// Directory containing pre-generated TLS certs/keys for the ephemeral installer
	ClusterTLSCertOverrideDir string `envconfig:"EPHEMERAL_INSTALLER_CLUSTER_TLS_CERTS_OVERRIDE_DIR" default:""`
}

type installGenerator struct {
	Config
	log              logrus.FieldLogger
	s3Client         s3wrapper.API
	workDir          string
	providerRegistry registry.ProviderRegistry
	manifestApi      manifestsapi.ManifestsAPI
}

func New(log logrus.FieldLogger, s3Client s3wrapper.API, cfg Config, workDir string,
	providerRegistry registry.ProviderRegistry, manifestApi manifestsapi.ManifestsAPI) *installGenerator {
	return &installGenerator{
		Config:           cfg,
		log:              log,
		s3Client:         s3Client,
		workDir:          filepath.Join(workDir, "install-config-generate"),
		providerRegistry: providerRegistry,
		manifestApi:      manifestApi,
	}
}

// GenerateInstallConfig creates install config and ignition files
func (k *installGenerator) GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte, releaseImage, installerReleaseImageOverride string) error {
	log := logutil.FromContext(ctx, k.log)
	err := os.MkdirAll(k.workDir, 0o755)
	if err != nil {
		return err
	}
	clusterWorkDir, err := os.MkdirTemp(k.workDir, cluster.ID.String()+".")
	if err != nil {
		return err
	}
	defer func() {
		if removeError := os.RemoveAll(clusterWorkDir); removeError != nil {
			log.WithError(removeError).Error("Failed to clean up generated ignition directory")
		}
	}()

	installerCacheDir := filepath.Join(k.workDir, "installercache")

	// runs openshift-install to generate ignition files, then modifies them as necessary
	var generator ignition.Generator
	if k.Config.DummyIgnition {
		generator = ignition.NewDummyGenerator(clusterWorkDir, &cluster, k.s3Client, log)
	} else {
		generator = ignition.NewGenerator(clusterWorkDir, installerCacheDir, &cluster, releaseImage, k.Config.ReleaseImageMirror,
			k.Config.ServiceCACertPath, k.Config.InstallInvoker, k.s3Client, log, k.providerRegistry, installerReleaseImageOverride, k.Config.ClusterTLSCertOverrideDir, k.InstallerCacheCapacity, k.manifestApi)
	}
	err = generator.Generate(ctx, cfg)
	if err != nil {
		return err
	}

	// upload files to S3
	err = generator.UploadToS3(ctx)
	if err != nil {
		return err
	}

	return nil
}
