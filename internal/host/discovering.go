package host

import (
	"context"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewDiscoveringState(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator) *discoveringState {
	return &discoveringState{
		baseState: baseState{
			log: log,
			db:  db,
		},
		hwValidator: hwValidator,
	}
}

type discoveringState struct {
	baseState
	hwValidator hardware.Validator
}

func (d *discoveringState) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	h.HardwareInfo = hwInfo
	return updateHwInfo(logutil.FromContext(ctx, d.log), d.hwValidator, h, d.db)
}

func (d *discoveringState) UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error) {
	h.Inventory = inventory
	return updateStateFromInventory(logutil.FromContext(ctx, d.log), d.hwValidator, h, d.db)
}

func (d *discoveringState) UpdateRole(ctx context.Context, h *models.Host, role string, db *gorm.DB) (*UpdateReply, error) {
	cdb := d.db
	if db != nil {
		cdb = db
	}
	return updateStateWithParams(logutil.FromContext(ctx, d.log), HostStatusDiscovering, statusInfoDiscovering, h, cdb, "role", role)
}

func (d *discoveringState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	if db == nil {
		db = d.db
	}
	reply, err := updateByKeepAlive(logutil.FromContext(ctx, d.log), h, db)
	if err != nil || reply.IsChanged || h.Inventory == "" {
		return reply, err
	}
	return updateStateFromInventory(logutil.FromContext(ctx, d.log), d.hwValidator, h, db)
}

func (d *discoveringState) Install(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to install host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (d *discoveringState) EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     HostStatusDiscovering,
		IsChanged: false,
	}, nil
}

func (d *discoveringState) DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return updateState(logutil.FromContext(ctx, d.log), HostStatusDisabled, statusInfoDisabled, h, d.db)
}
