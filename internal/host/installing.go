package host

import (
	"context"

	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
)

func NewInstallingState(log logrus.FieldLogger, db *gorm.DB) *installingState {
	return &installingState{
		log: log,
		db:  db,
	}
}

type installingState baseState

func (i *installingState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*models.Host, error) {
	// State in the same state
	return h, nil
}
