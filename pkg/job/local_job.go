package job

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/openshift/assisted-service/internal/network"
	"github.com/pkg/errors"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/generator"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=local_job.go -package=job -destination=mock_local_job.go
type LocalJob interface {
	Execute(pythonCommand string, pythonFilePath string, envVars []string, log logrus.FieldLogger) error
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

func (j *localJob) Execute(pythonCommand string, pythonFilePath string, envVars []string, log logrus.FieldLogger) error {
	cmd := exec.Command(pythonCommand, pythonFilePath)
	cmd.Env = envVars
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		log.Infoln("envVars: " + strings.Join(envVars, ","))
		log.WithError(err).Errorf(pythonFilePath)
		return err
	}
	log.Infoln(cmd.Stdout)
	return nil
}

// creates install config
func (j *localJob) GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte) error {
	log := logutil.FromContext(ctx, j.log)
	encodedDhcpFileContents, err := network.GetEncodedDhcpParamFileContents(&cluster)
	if err != nil {
		wrapped := errors.Wrapf(err, "Could not create DHCP encoded file")
		log.WithError(wrapped).Errorf("GenerateInstallConfig")
		return wrapped
	}
	envVars := append(os.Environ(),
		"INSTALLER_CONFIG="+string(cfg),
		"INVENTORY_ENDPOINT="+strings.TrimSpace(j.Config.ServiceBaseURL)+"/api/assisted-install/v1",
		"IMAGE_NAME="+j.Config.IgnitionGenerator,
		"CLUSTER_ID="+cluster.ID.String(),
		"OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE="+j.Config.ReleaseImage,
		"WORK_DIR=/data",
		"SKIP_CERT_VERIFICATION="+strconv.FormatBool(j.Config.SkipCertVerification),
	)
	if encodedDhcpFileContents != "" {
		envVars = append(envVars, "DHCP_ALLOCATION_FILE="+encodedDhcpFileContents)
	}
	return j.Execute("python3", "./data/render_files.py", envVars, log)
}

func (j *localJob) AbortInstallConfig(ctx context.Context, cluster common.Cluster) error {
	// no job to abort
	return nil
}

func (j *localJob) UploadBaseISO() error {
	return nil
}
