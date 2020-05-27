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

func (i *insufficientState) RefreshStatus(ctx context.Context, c *common.Cluster, db *gorm.DB) (*UpdateReply, error) {

	log := logutil.FromContext(ctx, i.log)

	if err := db.Preload("Hosts").First(&c, "id = ?", c.ID).Error; err != nil {
		return &UpdateReply{
			State:     clusterStatusInsufficient,
			IsChanged: false}, errors.Errorf("cluster %s not found", c.ID)
	}
	mappedMastersByRole := mapMasterHostsByStatus(c)

	// Cluster is ready
	mastersInKnown, ok := mappedMastersByRole[intenralhost.HostStatusKnown]
	if ok && len(mastersInKnown) == minHostsNeededForInstallation && c.APIVip != "" && c.IngressVip != "" {
		log.Infof("Cluster %s has %d known master hosts, cluster is ready.", c.ID, minHostsNeededForInstallation)
		return updateState(clusterStatusReady, statusInfoReady, c, db, log)

		//cluster is still insufficient
	} else {
		return &UpdateReply{State: clusterStatusInsufficient,
			IsChanged: false}, nil
	}
}
