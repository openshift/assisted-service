package cluster

import (
	"context"

	"github.com/filanov/bm-inventory/internal/common"

	"github.com/sirupsen/logrus"

	intenralhost "github.com/filanov/bm-inventory/internal/host"
	logutil "github.com/filanov/bm-inventory/pkg/log"

	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
)

func NewInsufficientState(log logrus.FieldLogger, db *gorm.DB) *insufficientState {
	return &insufficientState{
		log: log,
		db:  db,
	}
}

type insufficientState baseState

func (i *insufficientState) RefreshStatus(ctx context.Context, c *common.Cluster, db *gorm.DB) (*common.Cluster, error) {

	log := logutil.FromContext(ctx, i.log)

	if err := db.Preload("Hosts").First(&c, "id = ?", c.ID).Error; err != nil {
		return nil, errors.Errorf("cluster %s not found", c.ID)
	}
	mappedMastersByRole := mapMasterHostsByStatus(c)

	manager := intenralhost.NewManager(log, db, nil, nil, nil, nil)
	if isPendingUserResetRequired(c, manager) {
		log.Infof("Setting cluster: %s hosts to status: %s",
			c.ID, intenralhost.HostStatusInstallingPendingUserAction)
		if err := setPendingUserReset(ctx, c, db, manager); err != nil {
			return nil, errors.Wrapf(err, "failed setting cluster: %s hosts to status: %s",
				c.ID, intenralhost.HostStatusInstallingPendingUserAction)
		}
		return c, nil
	}

	// Cluster is ready
	mastersInKnown, ok := mappedMastersByRole[intenralhost.HostStatusKnown]
	if ok && len(mastersInKnown) == minHostsNeededForInstallation && c.APIVip != "" && c.IngressVip != "" {
		log.Infof("Cluster %s has %d known master hosts, cluster is ready.", c.ID, minHostsNeededForInstallation)
		return updateClusterStatus(log, db, *c.ID, *c.Status, clusterStatusReady, statusInfoReady)

		//cluster is still insufficient
	} else {
		return c, nil
	}
}

func isPendingUserResetRequired(c *common.Cluster, manager *intenralhost.Manager) bool {
	for _, h := range c.Hosts {
		if manager.IsRequireUserActionReset(h) {
			return true
		}
	}
	return false
}

func setPendingUserReset(ctx context.Context, c *common.Cluster, db *gorm.DB, manager *intenralhost.Manager) error {
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
		if err := manager.ResetPendingUserAction(ctx, h, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	txSuccess = true
	return nil
}
