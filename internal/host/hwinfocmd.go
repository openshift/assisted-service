package host

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
)

type hwInfoCmd struct {
	baseCmd
	hardwareInfoImage string
}

func NewHwInfoCmd(log logrus.FieldLogger, hardwareInfoImage string) *hwInfoCmd {
	return &hwInfoCmd{
		baseCmd:           baseCmd{log: log},
		hardwareInfoImage: hardwareInfoImage,
	}
}

func (h *hwInfoCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	step := &models.Step{}
	step.StepType = models.StepTypeHardwareInfo
	step.Command = "podman"
	step.Args = []string{
		"run", "--privileged", "--net=host", "--rm", "--quiet",
		"-v", "/var/log:/var/log",
		"-v", "/run/systemd/journal/socket:/run/systemd/journal/socket",
		h.hardwareInfoImage,
	}
	return step, nil
}
