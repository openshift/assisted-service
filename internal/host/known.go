package host

import (
	"context"

	"github.com/filanov/bm-inventory/internal/connectivity"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

func NewKnownState(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, connectivityValidator connectivity.Validator) *knownState {
	return &knownState{
		baseState: baseState{
			log: log,
			db:  db,
		},
		hwValidator:           hwValidator,
		connectivityValidator: connectivityValidator,
	}
}

type knownState struct {
	baseState
	hwValidator           hardware.Validator
	connectivityValidator connectivity.Validator
}

func (k *knownState) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	return checkAndUpdateSufficientHost(logutil.FromContext(ctx, k.log), h, db, k.hwValidator, k.connectivityValidator)
}
