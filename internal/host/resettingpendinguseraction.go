package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

func NewResettingPendingUserActionState(log logrus.FieldLogger, db *gorm.DB) *resettingPendingUserAction {
	return &resettingPendingUserAction{
		log: log,
		db:  db,
	}
}

type resettingPendingUserAction baseState

func (r *resettingPendingUserAction) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*models.Host, error) {
	// State in the same state
	return h, nil
}
