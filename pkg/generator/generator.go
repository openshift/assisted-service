package generator

import (
	"context"
	"os"
	"path/filepath"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/pkg/auth"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
)

type InstallConfigGenerator interface {
	GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte, releaseImage, installerReleaseImageOverride string) error
}

//go:generate mockgen --build_flags=--mod=mod -package generator -destination mock_install_config.go . ISOInstallConfigGenerator
type ISOInstallConfigGenerator interface {
	InstallConfigGenerator
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
	authHandler      auth.Authenticator
	log              logrus.FieldLogger
	s3Client         s3wrapper.API
	workDir          string
	providerRegistry registry.ProviderRegistry
}

func New(log logrus.FieldLogger, s3Client s3wrapper.API, cfg Config, workDir string,
	providerRegistry registry.ProviderRegistry, auth auth.Authenticator) *installGenerator {
	return &installGenerator{
		Config:           cfg,
		authHandler:      auth,
		log:              log,
		s3Client:         s3Client,
		workDir:          filepath.Join(workDir, "install-config-generate"),
		providerRegistry: providerRegistry,
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
	installerCacheDir := filepath.Join(k.workDir, "installercache")
	defer func() {
		// keep results in case of failure so a human can debug
		if err != nil {
			debugPath := filepath.Join(k.workDir, cluster.ID.String()+"-failed")
			// remove any prior failed results
			err2 := os.RemoveAll(debugPath)
			if err2 != nil && !os.IsNotExist(err2) {
				log.WithError(err).Errorf("Could not remove previous directory with failed config results: %s", debugPath)
				return
			}
			err2 = os.Rename(clusterWorkDir, debugPath)
			if err2 != nil {
				log.WithError(err).Errorf("Could not rename %s to %s", clusterWorkDir, debugPath)
				return
			}
			return
		}
		err2 := os.RemoveAll(clusterWorkDir)
		if err2 != nil {
			log.WithError(err).Error("Failed to clean up generated ignition directory")
		}
	}()

	// runs openshift-install to generate ignition files, then modifies them as necessary
	var generator ignition.Generator
	if k.Config.DummyIgnition {
		generator = ignition.NewDummyGenerator(clusterWorkDir, &cluster, k.s3Client, log)
	} else {
		generator = ignition.NewGenerator(clusterWorkDir, installerCacheDir, &cluster, releaseImage, k.Config.ReleaseImageMirror,
			k.Config.ServiceCACertPath, k.Config.InstallInvoker, k.s3Client, log, k.providerRegistry, installerReleaseImageOverride, k.Config.ClusterTLSCertOverrideDir, k.InstallerCacheCapacity)
	}
	err = generator.Generate(ctx, cfg, k.authHandler.AuthType())
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
