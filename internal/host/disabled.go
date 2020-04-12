package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewDisabledState(log logrus.FieldLogger, db *gorm.DB) *disabledState {
	return &disabledState{
		log: log,
		db:  db,
	}
}

type disabledState baseState

func (d *disabledState) RegisterHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to register host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (d *disabledState) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update hardware info to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (d *disabledState) UpdateRole(ctx context.Context, h *models.Host, role string, db *gorm.DB) (*UpdateReply, error) {
	cdb := d.db
	if db != nil {
		cdb = db
	}
	return updateStateWithParams(logutil.FromContext(ctx, d.log), hostStatusDisabled, statusInfoDisabled, h, cdb,
		"role", role)
}

func (d *disabledState) RefreshStatus(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     hostStatusDisabled,
		IsChanged: false,
	}, nil
}

func (d *disabledState) Install(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to install host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (d *disabledState) EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return updateStateWithParams(logutil.FromContext(ctx, d.log), hostStatusDiscovering, "", h, d.db,
		"hardware_info", "")
}

func (d *disabledState) DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     hostStatusDisabled,
		IsChanged: false,
	}, nil
}
