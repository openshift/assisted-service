package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewResettingState(log logrus.FieldLogger, db *gorm.DB) *resettingState {
	return &resettingState{
		log: log,
		db:  db,
	}
}

type resettingState baseState

func (r *resettingState) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update hardware info to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (r *resettingState) UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update inventory to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (r *resettingState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return &UpdateReply{
		State:     HostStatusResetting,
		IsChanged: false,
	}, nil
}

func (r *resettingState) Install(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to install host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (r *resettingState) EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return &UpdateReply{
		State:     HostStatusResetting,
		IsChanged: false,
	}, nil
}

func (r *resettingState) DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to disable host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}
