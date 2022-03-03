package hostcommands

import (
	"context"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type resetInstallationCmd struct {
	baseCmd
}

func NewResetInstallationCmd(log logrus.FieldLogger) *resetInstallationCmd {
	return &resetInstallationCmd{
		baseCmd: baseCmd{log: log},
	}
}

func (h *resetInstallationCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	step := &models.Step{}
	step.StepType = models.StepTypeResetInstallation
	step.Command = ""
	step.Args = []string{}
	return []*models.Step{step}, nil
}
