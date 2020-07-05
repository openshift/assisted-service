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

func NewInsufficientState(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, connectivityValidator connectivity.Validator) *insufficientState {
	return &insufficientState{
		baseState: baseState{
			log: log,
			db:  db,
		},
		hwValidator:           hwValidator,
		connectivityValidator: connectivityValidator,
	}
}

type insufficientState struct {
	baseState
	hwValidator           hardware.Validator
	connectivityValidator connectivity.Validator
}

func (i *insufficientState) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	h.HardwareInfo = hwInfo
	return updateHwInfo(logutil.FromContext(ctx, i.log), i.hwValidator, h, i.db)
}

func (d *insufficientState) UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error) {
	h.Inventory = inventory
	return updateInventory(logutil.FromContext(ctx, d.log), d.hwValidator, h, d.db)
}

func (i *insufficientState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return checkAndUpdateSufficientHost(logutil.FromContext(ctx, i.log), h, db, i.hwValidator, i.connectivityValidator)
}

func (i *insufficientState) EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     HostStatusInsufficient,
		IsChanged: false,
	}, nil
}

func (i *insufficientState) DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return updateState(logutil.FromContext(ctx, i.log), HostStatusDisabled, statusInfoDisabled, h, i.db)
}
