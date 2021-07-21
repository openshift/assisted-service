package hostcommands

import (
	"context"

	models "github.com/openshift/assisted-service/models/v1"
	"github.com/sirupsen/logrus"
)

type stopInstallationCmd struct {
	baseCmd
}

func NewStopInstallationCmd(log logrus.FieldLogger) *stopInstallationCmd {
	return &stopInstallationCmd{
		baseCmd: baseCmd{log: log},
	}
}

func (h *stopInstallationCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	command := "/usr/bin/podman"

	step := &models.Step{
		StepType: models.StepTypeExecute,
		Command:  command,
		Args: []string{
			"stop", "-i", "-t", "5", "assisted-installer",
		},
	}

	return []*models.Step{step}, nil
}
