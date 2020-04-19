package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewInstalledState(log logrus.FieldLogger, db *gorm.DB) *installedState {
	return &installedState{
		log: log,
		db:  db,
	}
}

type installedState baseState

func (i *installedState) RegisterHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to register host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (i *installedState) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update hardware info to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (i *installedState) UpdateRole(ctx context.Context, h *models.Host, role string, db *gorm.DB) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to set role host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (i *installedState) RefreshStatus(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     HostStatusInstalled,
		IsChanged: false,
	}, nil
}

func (i *installedState) Install(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to install host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (i *installedState) EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     HostStatusInstalled,
		IsChanged: false,
	}, nil
}

func (i *installedState) DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to disable host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}
