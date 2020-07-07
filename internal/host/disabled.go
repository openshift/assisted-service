package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

func NewDisabledState(log logrus.FieldLogger, db *gorm.DB) *disabledState {
	return &disabledState{
		log: log,
		db:  db,
	}
}

type disabledState baseState

func (d *disabledState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return defaultReply(h)
}
