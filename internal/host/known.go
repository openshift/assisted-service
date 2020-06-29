package host

import (
	"context"

	"github.com/filanov/bm-inventory/internal/connectivity"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

func NewKnownState(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, connectivityValidator connectivity.Validator) *knownState {
	return &knownState{
		baseState: baseState{
			log: log,
			db:  db,
		},
		hwValidator:           hwValidator,
		connectivityValidator: connectivityValidator,
	}
}

type knownState struct {
	baseState
	hwValidator           hardware.Validator
	connectivityValidator connectivity.Validator
}

func (k *knownState) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	h.HardwareInfo = hwInfo
	return updateHwInfo(logutil.FromContext(ctx, k.log), k.hwValidator, h, k.db)
}

func (k *knownState) UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error) {
	h.Inventory = inventory
	return updateInventory(logutil.FromContext(ctx, k.log), k.hwValidator, h, k.db)
}

func (k *knownState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return checkAndUpdateSufficientHost(logutil.FromContext(ctx, k.log), h, db, k.hwValidator, k.connectivityValidator)
}

func (k *knownState) Install(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	if !funk.ContainsString([]string{models.HostRoleMaster, models.HostRoleWorker}, h.Role) {
		return nil, errors.Errorf("unable to install host <%s> without valid role", h.ID)
	}
	cdb := k.db
	if db != nil {
		cdb = db
	}
	return updateState(logutil.FromContext(ctx, k.log), HostStatusInstalling, statusInfoInstalling, h, cdb)
}

func (k *knownState) EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     HostStatusKnown,
		IsChanged: false,
	}, nil
}

func (k *knownState) DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return updateState(logutil.FromContext(ctx, k.log), HostStatusDisabled, statusInfoDisabled, h, k.db)
}
