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

func (e *errorState) RegisterHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to register host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (e *errorState) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update hardware info to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (e *errorState) UpdateRole(ctx context.Context, h *models.Host, role string, db *gorm.DB) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update role to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (e *errorState) RefreshStatus(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     hostStatusError,
		IsChanged: false,
	}, nil
}

func (e *errorState) Install(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to install host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (e *errorState) EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	// State in the same state
	return &UpdateReply{
		State:     hostStatusError,
		IsChanged: false,
	}, nil
}

func (e *errorState) DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to disable host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}
