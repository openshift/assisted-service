package host

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
)

type inventoryCmd struct {
	baseCmd
	inventoryImage string
}

func NewInventoryCmd(log logrus.FieldLogger, inventoryImage string) *inventoryCmd {
	return &inventoryCmd{
		baseCmd:        baseCmd{log: log},
		inventoryImage: inventoryImage,
	}
}

func (h *inventoryCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	step := &models.Step{
		StepType: models.StepTypeInventory,
		Command:  "podman",
		Args: []string{
			"run", "--privileged", "--net=host", "--rm", "--quiet",
			"-v", "/var/log:/var/log",
			"-v", "/run/udev:/run/udev",
			"-v", "/dev/disk:/dev/disk",
			"-v", "/run/systemd/journal/socket:/run/systemd/journal/socket",
			h.inventoryImage,
			"inventory",
		},
	}
	return step, nil
}
