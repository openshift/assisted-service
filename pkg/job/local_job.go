package job

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/openshift/assisted-service/internal/ignition"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/pkg/generator"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

type LocalJob interface {
	generator.ISOInstallConfigGenerator
}

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
	if j.Config.DummyIgnition {
		generator = ignition.NewDummyGenerator(workDir, &cluster, log)
	} else {
		generator = ignition.NewGenerator(workDir, installerCacheDir, &cluster, releaseImage, log)
	}
	err = generator.Generate(cfg)
	if err != nil {
		return err
	}

	return nil
}

func (j *localJob) AbortInstallConfig(ctx context.Context, cluster common.Cluster) error {
	// no job to abort
	return nil
}

func (j *localJob) GenerateISO(ctx context.Context, cluster common.Cluster, jobName string, imageName string, ignitionConfig string, eventsHandler events.Handler) error {
	log := logutil.FromContext(ctx, j.log)
	workDir := j.Config.WorkDir
	// #nosec G204
	cmd := exec.Command(workDir + "/assisted-iso-create")
	cmd.Env = append(os.Environ(),
		"IGNITION_CONFIG="+ignitionConfig,
		"IMAGE_NAME="+imageName,
		"COREOS_IMAGE="+workDir+"/livecd.iso",
		"USE_S3=false",
		"WORK_DIR="+workDir,
	)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		log.Errorf("assisted-iso-create failed: %s", out.String())
		return err
	}
	return nil
}
