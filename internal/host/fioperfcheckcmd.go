package host

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

const (
	FioPerfCheckCmdExitCode int64 = 222
	FioDurationThresholdMs  int64 = 10
)

type FioPerfCheckConfig struct {
	ServiceBaseURL       string
	ClusterID            string
	HostID               string
	UseCustomCACert      bool
	FioPerfCheckImage    string
	SkipCertVerification bool
	Path                 string
	DurationThresholdMs  int64
}

type fioPerfCheckCmd struct {
	baseCmd
	config FioPerfCheckConfig
}

func NewFioPerfCheckCmd(log logrus.FieldLogger, config FioPerfCheckConfig) *fioPerfCheckCmd {
	return &fioPerfCheckCmd{
		baseCmd: baseCmd{log: log},
		config:  config,
	}
}

func (c *fioPerfCheckCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	args, err := c.GetArgs()
	if err != nil {
		return nil, err
	}

	step := &models.Step{
		StepType: models.StepTypeFioPerfCheck,
		Command:  "podman",
		Args:     args,
	}
	return []*models.Step{step}, nil
}

func (c *fioPerfCheckCmd) GetArgs() ([]string, error) {
	exitCode := FioPerfCheckCmdExitCode
	request := models.FioPerfCheckRequest{
		Path:                &c.config.Path,
		DurationThresholdMs: &c.config.DurationThresholdMs,
		ExitCode:            &exitCode,
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		c.log.WithError(err).Errorf("failed to marshal FioPerfCheckRequest")
		return nil, err
	}

	arguments := []string{
		"run", "--privileged", "--net=host", "--rm", "--quiet", "--name=assisted-installer",
		"-v", "/dev:/dev:rw",
		"-v", "/var/log:/var/log",
		"-v", "/run/systemd/journal/socket:/run/systemd/journal/socket",
	}

	if c.config.UseCustomCACert {
		arguments = append(arguments, "-v", fmt.Sprintf("%s:%s", common.HostCACertPath, common.HostCACertPath))
	}

	arguments = append(arguments,
		"--env", "PULL_SECRET_TOKEN",
		"--env", "HTTP_PROXY", "--env", "HTTPS_PROXY", "--env", "NO_PROXY",
		"--env", "http_proxy", "--env", "https_proxy", "--env", "no_proxy",
		c.config.FioPerfCheckImage, "fio_perf_check",
		"--url", strings.TrimSpace(c.config.ServiceBaseURL), "--cluster-id", c.config.ClusterID, "--host-id", c.config.HostID,
		"--agent-version", c.config.FioPerfCheckImage)
	if c.config.SkipCertVerification {
		arguments = append(arguments, "--insecure")
	}

	if c.config.UseCustomCACert {
		arguments = append(arguments, "--cacert", common.HostCACertPath)
	}

	arguments = append(arguments, strconv.Quote(string(requestBytes)))

	return arguments, nil
}

func (c *fioPerfCheckCmd) GetCommandString() string {
	args, err := c.GetArgs()
	if err != nil {
		return ""
	}

	return fmt.Sprintf("podman %s ; ", strings.Join(args, " "))
}
