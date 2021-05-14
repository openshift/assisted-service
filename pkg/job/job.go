package job

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

type Config struct {
	ServiceCACertPath  string `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	ServiceIPs         string `envconfig:"SERVICE_IPS" default:""`
	ReleaseImageMirror string
	WorkDir            string `envconfig:"WORK_DIR" default:"/data/"`
	DummyIgnition      bool   `envconfig:"DUMMY_IGNITION"`
}

func New(log logrus.FieldLogger, s3Client s3wrapper.API, cfg Config, operatorsApi operators.API) *kubeJob {
	return &kubeJob{
		Config:       cfg,
		log:          log,
		s3Client:     s3Client,
		operatorsApi: operatorsApi,
	}
}

type kubeJob struct {
	Config
	log          logrus.FieldLogger
	s3Client     s3wrapper.API
	operatorsApi operators.API
}

// GenerateInstallConfig creates install config and ignition files
func (k *kubeJob) GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte, releaseImage string) error {
	log := logutil.FromContext(ctx, k.log)
	workDir := filepath.Join(k.Config.WorkDir, cluster.ID.String())
	installerCacheDir := filepath.Join(k.Config.WorkDir, "installercache")
	err := os.Mkdir(workDir, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}
	defer func() {
		// keep results in case of failure so a human can debug
		if err != nil {
			debugPath := filepath.Join(k.Config.WorkDir, cluster.ID.String()+"-failed")
			// remove any prior failed results
			err2 := os.RemoveAll(debugPath)
			if err2 != nil && !os.IsNotExist(err2) {
				log.WithError(err).Errorf("Could not remove previous directory with failed config results: %s", debugPath)
				return
			}
			err2 = os.Rename(workDir, debugPath)
			if err2 != nil {
				log.WithError(err).Errorf("Could not rename %s to %s", workDir, debugPath)
				return
			}
			return
		}
		err2 := os.RemoveAll(workDir)
		if err2 != nil {
			log.WithError(err).Error("Failed to clean up generated ignition directory")
		}
	}()

	// runs openshift-install to generate ignition files, then modifies them as necessary
	var generator ignition.Generator
	if k.Config.DummyIgnition {
		generator = ignition.NewDummyGenerator(workDir, &cluster, k.s3Client, log)
	} else {
		generator = ignition.NewGenerator(workDir, installerCacheDir, &cluster, releaseImage, "", k.Config.ServiceCACertPath, k.s3Client, log, k.operatorsApi)
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
