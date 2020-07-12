package host

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
)

type stopInstallationCmd struct {
	baseCmd
}

func NewStopInstallationCmd(log logrus.FieldLogger) *stopInstallationCmd {
	return &stopInstallationCmd{
		baseCmd: baseCmd{log: log},
	}
}

func (h *stopInstallationCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	step := &models.Step{
		StepType: models.StepTypeExecute,
		Command:  "/usr/bin/podman",
		Args: []string{
			"kill", "--all",
		},
	}
	return step, nil
}
