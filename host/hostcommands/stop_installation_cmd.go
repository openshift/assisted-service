package hostcommands

import (
	"context"

	"github.com/openshift/assisted-service/models"
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
	step := &models.Step{
		StepType: models.StepTypeStopInstallation,
		Args:     []string{},
	}

	return []*models.Step{step}, nil
}
