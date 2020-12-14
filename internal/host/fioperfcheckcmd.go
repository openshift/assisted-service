package host

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

const (
	FioPerfCheckCmdExitCode int64 = 222
	FioDurationThreshold    int64 = 20
)

type fioPerfCheckCmd struct {
	baseCmd
	fioPerfCheckImage string
	path              string
	durationThreshold int64
}

func NewFioPerfCheckCmd(log logrus.FieldLogger, fioPerfCheckImage string, path string, durationThreshold int64) *fioPerfCheckCmd {
	return &fioPerfCheckCmd{
		baseCmd:           baseCmd{log: log},
		fioPerfCheckImage: fioPerfCheckImage,
		path:              path,
		durationThreshold: durationThreshold,
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
		Path:              &c.path,
		DurationThreshold: &c.durationThreshold,
		ExitCode:          &exitCode,
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		c.log.WithError(err).Errorf("failed to marshal FioPerfCheckRequest")
		return nil, err
	}

	return []string{
		"run", "--privileged", "--net=host", "--rm", "--quiet",
		"-v", "/dev:/dev:rw",
		"-v", "/var/log:/var/log",
		"-v", "/run/systemd/journal/socket:/run/systemd/journal/socket",
		c.fioPerfCheckImage,
		"fio_perf_check",
		strconv.Quote(string(requestBytes)),
	}, nil
}

func (c *fioPerfCheckCmd) GetCommandString() string {
	args, err := c.GetArgs()
	if err != nil {
		return ""
	}

	return fmt.Sprintf("podman %s && ", strings.Join(args, " "))
}
