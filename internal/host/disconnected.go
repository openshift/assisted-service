package host

import (
	"context"
	"time"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

func NewDisconnectedState(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator) *disconnectedState {
	return &disconnectedState{
		baseState: baseState{
			log: log,
			db:  db,
		},
		hwValidator: hwValidator,
	}
}

type disconnectedState struct {
	baseState
	hwValidator hardware.Validator
}

func (d *disconnectedState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	log := logutil.FromContext(ctx, d.log)
	if time.Since(time.Time(h.CheckedInAt)) < 3*time.Minute {
		return updateHostState(log, HostStatusDiscovering, statusInfoDiscovering, h, d.db)
	}
	// Stay in the same state
	return defaultReply(h)
}
