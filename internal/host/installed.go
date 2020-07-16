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

func (i *installedState) UpdateInventory(ctx context.Context, h *models.Host, inventory string) error {
	return errors.Errorf("unable to update inventory to host <%s> in <%s> status",
		h.ID, swag.StringValue(h.Status))
}

func (i *installedState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*models.Host, error) {
	// State in the same state
	return h, nil
}
