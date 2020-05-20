package host

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
)

type inventoryCmd baseCmd

func NewInventoryCmd(log logrus.FieldLogger) *inventoryCmd {
	return &inventoryCmd{
		log: log,
	}
}

func (h *inventoryCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	step := &models.Step{}
	step.StepType = models.StepTypeInventory
	return step, nil
}
