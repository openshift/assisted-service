package host

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
)

type resetAgentCmd struct {
	baseCmd
}

func NewResetAgentCmd(log logrus.FieldLogger) *resetAgentCmd {
	return &resetAgentCmd{
		baseCmd: baseCmd{log: log},
	}
}

func (h *resetAgentCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	step := &models.Step{
		StepType: models.StepTypeResetAgent,
		Command:  "systemctl",
		Args: []string{
			"restart", "agent",
		},
	}
	return step, nil
}
