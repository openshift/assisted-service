package generator

import (
	"context"
	"os"
	"path/filepath"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/operators"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
)

type InstallConfigGenerator interface {
	GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte, releaseImage string) error
}

//go:generate mockgen -package generator -destination mock_install_config.go . ISOInstallConfigGenerator
type ISOInstallConfigGenerator interface {
	InstallConfigGenerator
}

type Config struct {
	ServiceCACertPath  string `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	ServiceIPs         string `envconfig:"SERVICE_IPS" default:""`
	ReleaseImageMirror string
	DummyIgnition      bool   `envconfig:"DUMMY_IGNITION"`
	InstallInvoker     string `envconfig:"INSTALL_INVOKER" default:"assisted-installer"`
}

type installGenerator struct {
	Config
	log          logrus.FieldLogger
	s3Client     s3wrapper.API
	operatorsApi operators.API
	workDir      string
}

func New(log logrus.FieldLogger, s3Client s3wrapper.API, cfg Config, workDir string, operatorsApi operators.API) *installGenerator {
	return &installGenerator{
		Config:       cfg,
		log:          log,
		s3Client:     s3Client,
		operatorsApi: operatorsApi,
		workDir:      filepath.Join(workDir, "install-config-generate"),
	}
}

// GenerateInstallConfig creates install config and ignition files
func (k *installGenerator) GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte, releaseImage string) error {
	log := logutil.FromContext(ctx, k.log)
	clusterWorkDir := filepath.Join(k.workDir, cluster.ID.String())
	installerCacheDir := filepath.Join(k.workDir, "installercache")
	err := os.MkdirAll(clusterWorkDir, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}
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
		generator = ignition.NewGenerator(clusterWorkDir, installerCacheDir, &cluster, releaseImage, k.Config.ReleaseImageMirror, k.Config.ServiceCACertPath, k.Config.InstallInvoker, k.s3Client, log, k.operatorsApi)
	}
	err = generator.Generate(ctx, cfg)
	if err != nil {
		return err
	}

	if k.Config.ServiceIPs != "" {
		err = generator.UpdateEtcHosts(k.Config.ServiceIPs)
		if err != nil {
			return err
		}
	}

	// upload files to S3
	err = generator.UploadToS3(ctx)
	if err != nil {
		return err
	}

	return nil
}
