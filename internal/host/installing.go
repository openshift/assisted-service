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

func NewInstallingState(log logrus.FieldLogger, db *gorm.DB) *installingState {
	return &installingState{
		log: log,
		db:  db,
	}
}

type installingState baseState

func (i *installingState) RegisterHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return updateStateWithParams(logutil.FromContext(ctx, i.log), HostStatusError,
		"host rebooted during installation process", h, i.db)
}

func (i *installingState) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update hardware info to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (i *installingState) UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update inventory to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (i *installingState) UpdateRole(ctx context.Context, h *models.Host, role string, db *gorm.DB) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update role to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (i *installingState) RefreshStatus(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     HostStatusInstalling,
		IsChanged: false,
	}, nil
}

func (i *installingState) Install(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	// TODO: maybe need to jump to the next sub installation state
	return nil, errors.Errorf("unable to install host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (i *installingState) EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     HostStatusInstalling,
		IsChanged: false,
	}, nil
}

func (i *installingState) DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to disable host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}
