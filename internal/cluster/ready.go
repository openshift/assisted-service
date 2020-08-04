package cluster

import (
	"context"

	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	intenralhost "github.com/openshift/assisted-service/internal/host"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

func NewReadyState(log logrus.FieldLogger, db *gorm.DB) *readyState {
	return &readyState{
		log: log,
		db:  db,
	}
}

type readyState baseState

var _ StateAPI = (*Manager)(nil)

func (r *readyState) RefreshStatus(ctx context.Context, c *common.Cluster, db *gorm.DB) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, r.log)

	mappedMastersByRole := mapMasterHostsByStatus(c)

	// Installation has started
	mastersInInstalling := mappedMastersByRole[intenralhost.HostStatusInstalling]
	mastersInInstallingInProgress := mappedMastersByRole[intenralhost.HostStatusInstallingInProgress]
	if len(mastersInInstalling) > 0 || len(mastersInInstallingInProgress) > 0 {
		return c, nil
	}

	// Cluster is insufficient
	mastersInKnown := mappedMastersByRole[intenralhost.HostStatusKnown]
	if len(mastersInKnown) != minHostsNeededForInstallation {
		log.Infof("Cluster %s dos not have exactly %d known master hosts, cluster is insufficient.", c.ID, minHostsNeededForInstallation)
		return updateClusterStatus(log, db, *c.ID, swag.StringValue(c.Status), clusterStatusInsufficient, statusInfoInsufficient)

		//cluster is still ready
	} else {
		return c, nil
	}
}
