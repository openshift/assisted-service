package cluster

import (
	"context"

	"github.com/sirupsen/logrus"

	host2 "github.com/filanov/bm-inventory/internal/host"
	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"

	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
)

func NewReadyState(log logrus.FieldLogger, db *gorm.DB) *readyState {
	return &readyState{
		log: log,
		db:  db,
	}
}

type readyState baseState

var _ StateAPI = (*Manager)(nil)

func (r *readyState) RefreshStatus(ctx context.Context, c *models.Cluster, db *gorm.DB) (*UpdateReply, error) {
	log := logutil.FromContext(ctx, r.log)

	if err := db.Preload("Hosts").First(&c, "id = ?", c.ID).Error; err != nil {
		return &UpdateReply{
			State:     clusterStatusInsufficient,
			IsChanged: false}, errors.Errorf("cluster %s not found", c.ID)
	}
	mappedMastersByRole := mapMasterHostsByStatus(c)

	// Cluster is insufficient
	mastersInKnown := mappedMastersByRole[host2.HostStatusKnown]
	if len(mastersInKnown) < minHostsNeededForInstallation {
		log.Infof("Cluster %s dos not have at least %d known master hosts, cluster is insufficient.", c.ID, minHostsNeededForInstallation)
		return updateState(clusterStatusInsufficient, c, db, log)

		//cluster is still ready
	} else {
		return &UpdateReply{State: clusterStatusReady,
			IsChanged: false}, nil
	}
}
