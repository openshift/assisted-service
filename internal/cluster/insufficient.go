package cluster

import (
	"context"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/internal/host"
	"github.com/filanov/bm-inventory/models"

	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewInsufficientState(log logrus.FieldLogger, db *gorm.DB, hostAPI host.API) *insufficientState {
	return &insufficientState{
		baseState: baseState{
			log: log,
			db:  db,
		},
		hostAPI: hostAPI,
	}
}

type insufficientState struct {
	baseState
	hostAPI host.API
}

func (i *insufficientState) RefreshStatus(ctx context.Context, c *common.Cluster, db *gorm.DB) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, i.log)

	mappedMastersByRole := mapMasterHostsByStatus(c)

	if i.isPendingUserResetRequired(c) {
		log.Infof("Setting cluster: %s hosts to status: %s",
			c.ID, models.HostStatusInstallingPendingUserAction)
		if err := i.setPendingUserReset(ctx, c, db); err != nil {
			return nil, errors.Wrapf(err, "failed setting cluster: %s hosts to status: %s",
				c.ID, models.HostStatusInstallingPendingUserAction)
		}
		return c, nil
	}

	// Cluster is ready
	mastersInKnown, ok := mappedMastersByRole[models.HostStatusKnown]
	if ok && len(mastersInKnown) == minHostsNeededForInstallation && c.APIVip != "" && c.IngressVip != "" {
		log.Infof("Cluster %s has %d known master hosts, cluster is ready.", c.ID, minHostsNeededForInstallation)
		return updateClusterStatus(log, db, *c.ID, swag.StringValue(c.Status), clusterStatusReady, statusInfoReady)

		//cluster is still insufficient
	} else {
		return c, nil
	}
}

func (i *insufficientState) isPendingUserResetRequired(c *common.Cluster) bool {
	for _, h := range c.Hosts {
		if i.hostAPI.IsRequireUserActionReset(h) {
			return true
		}
	}
	return false
}

func (i *insufficientState) setPendingUserReset(ctx context.Context, c *common.Cluster, db *gorm.DB) error {
	txSuccess := false
	tx := db.Begin()
	defer func() {
		if !txSuccess {
			tx.Rollback()
		}
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	for _, h := range c.Hosts {
		if err := i.hostAPI.ResetPendingUserAction(ctx, h, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	txSuccess = true
	return nil
}
