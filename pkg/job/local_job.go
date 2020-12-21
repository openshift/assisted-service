package job

import (
	"context"
	"os"
	"path/filepath"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/ignition"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
)

type localJob struct {
	Config
	log logrus.FieldLogger
}

func NewLocalJob(log logrus.FieldLogger, cfg Config) *localJob {
	return &localJob{
		Config: cfg,
		log:    log,
	}
}

// GenerateInstallConfig creates install config and ignition files
func (j *localJob) GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte, releaseImage string) error {
	log := logutil.FromContext(ctx, j.log)
	workDir := filepath.Join(j.Config.WorkDir, cluster.ID.String())
	installerCacheDir := filepath.Join(j.Config.WorkDir, "installercache")
	err := os.Mkdir(workDir, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}

	// runs openshift-install to generate ignition files, then modifies them as necessary
	var generator ignition.Generator
	s3Client := s3wrapper.NewFSClient(workDir, log)
	if s3Client == nil {
		log.Fatal("failed to create S3 file system client, ", err)
	}
	if j.Config.DummyIgnition {
		generator = ignition.NewDummyGenerator(workDir, &cluster, s3Client, log)
	} else {
		generator = ignition.NewGenerator(workDir, installerCacheDir, &cluster, releaseImage, j.Config.ReleaseImageMirror, j.Config.ServiceCACertPath, s3Client, log)
	}
	err = generator.Generate(ctx, cfg)
	if err != nil {
		return err
	}
	if j.Config.ServiceIPs != "" {
		err = generator.UpdateEtcHosts(j.Config.ServiceIPs)
		if err != nil {
			return err
		}
	}

	return nil
}

func (j *localJob) AbortInstallConfig(ctx context.Context, cluster common.Cluster) error {
	// no job to abort
	return nil
}

func (j *localJob) UploadBaseISO() error {
	return nil
}
