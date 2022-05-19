package hostcommands

import (
	"context"

	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
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

func (h *inventoryCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	inventoryCmd := &models.Step{
		StepType: models.StepTypeInventory,
		Args: []string{
			host.ID.String(),
		},
	}

	return []*models.Step{inventoryCmd}, nil
}
