package host

import (
	"context"

	"github.com/filanov/bm-inventory/internal/connectivity"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

func NewDiscoveringState(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, connectivityValidator connectivity.Validator) *discoveringState {
	return &discoveringState{
		baseState: baseState{
			log: log,
			db:  db,
		},
		hwValidator:           hwValidator,
		connectivityValidator: connectivityValidator,
	}
}

type discoveringState struct {
	baseState
	hwValidator           hardware.Validator
	connectivityValidator connectivity.Validator
}

func (d *discoveringState) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	h.HardwareInfo = hwInfo
	return updateHwInfo(logutil.FromContext(ctx, d.log), d.hwValidator, h, d.db)
}

func (d *discoveringState) UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error) {
	h.Inventory = inventory
	return updateInventory(logutil.FromContext(ctx, d.log), d.hwValidator, h, d.db)
}

func (d *discoveringState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return checkAndUpdateSufficientHost(logutil.FromContext(ctx, d.log), h, db, d.hwValidator, d.connectivityValidator)
}
