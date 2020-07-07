package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

func NewResettingState(log logrus.FieldLogger, db *gorm.DB) *resettingState {
	return &resettingState{
		log: log,
		db:  db,
	}
}

type resettingState baseState

func (r *resettingState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return &UpdateReply{
		State:     HostStatusResetting,
		IsChanged: false,
	}, nil
}
