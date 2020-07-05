package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewErrorState(log logrus.FieldLogger, db *gorm.DB) *errorState {
	return &errorState{
		log: log,
		db:  db,
	}
}

type errorState baseState

func (e *errorState) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update hardware info to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (i *errorState) UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update inventory to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (e *errorState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     HostStatusError,
		IsChanged: false,
	}, nil
}

func (e *errorState) EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     HostStatusError,
		IsChanged: false,
	}, nil
}

func (e *errorState) DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to disable host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}
