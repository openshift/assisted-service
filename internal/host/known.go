package host

import (
	"context"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewKnownState(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator) *knownState {
	return &knownState{
		baseState: baseState{
			log: log,
			db:  db,
		},
		hwValidator: hwValidator,
	}
}

type knownState struct {
	baseState
	hwValidator hardware.Validator
}

func (k *knownState) RegisterHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return updateStateWithParams(logutil.FromContext(ctx, k.log), HostStatusDiscovering, HostStatusDiscovering, h, k.db,
		"hardware_info", "")
}

func (k *knownState) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	h.HardwareInfo = hwInfo
	return updateHwInfo(logutil.FromContext(ctx, k.log), k.hwValidator, h, k.db)
}

func (k *knownState) UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error) {
	h.Inventory = inventory
	return updateInventory(logutil.FromContext(ctx, k.log), k.hwValidator, h, k.db)
}

func (k *knownState) UpdateRole(ctx context.Context, h *models.Host, role string, db *gorm.DB) (*UpdateReply, error) {
	log := logutil.FromContext(ctx, k.log)
	cdb := k.db
	if db != nil {
		cdb = db
	}
	h.Role = role
	reply, err := k.hwValidator.IsSufficient(h)
	if err != nil {
		return nil, err
	}
	if !reply.IsSufficient {
		return updateStateWithParams(log, HostStatusInsufficient, reply.Reason, h, cdb, "role", role)
	}
	return updateStateWithParams(log, HostStatusKnown, "", h, cdb, "role", role)
}

func (k *knownState) RefreshStatus(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return updateByKeepAlive(logutil.FromContext(ctx, k.log), h, k.db)
}

func (k *knownState) Install(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	if h.Role == "" {
		return nil, errors.Errorf("unable to install host <%s> without a role", h.ID)
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
