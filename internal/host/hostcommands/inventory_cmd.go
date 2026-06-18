package hostcommands

import (
	"context"
	"fmt"

	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type inventoryCmd struct {
	baseCmd
	inventoryMaxSize int64
	diskMinSize      int64
}

func NewInventoryCmd(log logrus.FieldLogger, inventoryMaxSize int64, diskMinSize int64) *inventoryCmd {
	return &inventoryCmd{
		baseCmd:          baseCmd{log: log},
		inventoryMaxSize: inventoryMaxSize,
		diskMinSize:      diskMinSize,
	}
}

func (h *inventoryCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	inventoryCmd := &models.Step{
		StepType: models.StepTypeInventory,
		Args: []string{
			host.ID.String(),
		},
	}

	if h.inventoryMaxSize > 0 {
		inventoryCmd.Args = append(inventoryCmd.Args, fmt.Sprintf("--output-max-size=%d", h.inventoryMaxSize))
	}

	if h.diskMinSize > 0 {
		inventoryCmd.Args = append(inventoryCmd.Args, fmt.Sprintf("--disk-min-size=%d", h.diskMinSize))
	}

	return []*models.Step{inventoryCmd}, nil
}
