package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
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

func (d *disabledState) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update hardware info to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (d *disabledState) UpdateInventory(ctx context.Context, h *models.Host, inventory string) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to update inventory to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (d *disabledState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return defaultReply(h)
}
