package cluster

import (
	context "context"
	"fmt"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"

	"github.com/filanov/bm-inventory/internal/common"
	intenralhost "github.com/filanov/bm-inventory/internal/host"

	logutil "github.com/filanov/bm-inventory/pkg/log"

	"github.com/sirupsen/logrus"

	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
)

func NewInstallingState(log logrus.FieldLogger, db *gorm.DB) *installingState {
	return &installingState{
		log: log,
		db:  db,
	}
}

type installingState baseState

var _ StateAPI = (*Manager)(nil)

func (i *installingState) RefreshStatus(ctx context.Context, c *common.Cluster, db *gorm.DB) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, i.log)
	installationState, StateInfo, err := i.getClusterInstallationState(ctx, c, db)
	if err != nil {
		return nil, errors.Errorf("couldn't determine cluster %s installation state", c.ID)
	}

	switch installationState {
	case models.ClusterStatusFinalizing:
		return updateClusterStatus(log, db, *c.ID, swag.StringValue(c.Status), models.ClusterStatusFinalizing, StateInfo)
	case clusterStatusInstalled:
		return updateClusterStatus(log, db, *c.ID, swag.StringValue(c.Status), clusterStatusInstalled, StateInfo)
	case clusterStatusError:
		return updateClusterStatus(log, db, *c.ID, swag.StringValue(c.Status), clusterStatusError, StateInfo)
	case clusterStatusInstalling:
		return c, nil
	}
	return nil, errors.Errorf("cluster % state transaction is not clear, installation state: %s ", c.ID, installationState)
}

func (i *installingState) getClusterInstallationState(ctx context.Context, c *common.Cluster, db *gorm.DB) (string, string, error) {
	log := logutil.FromContext(ctx, i.log)

	mappedMastersByRole := mapMasterHostsByStatus(c)

	// Cluster is in finalizing
	mastersInInstalled, ok := mappedMastersByRole[intenralhost.HostStatusInstalled]
	if ok && len(mastersInInstalled) >= minHostsNeededForInstallation {
		log.Infof("Cluster %s has at least %d installed hosts, cluster is installed.", c.ID, len(mastersInInstalled))
		return models.ClusterStatusFinalizing, statusInfoFinalizing, nil
	}

	// Cluster is installing
	mastersInSomeInstallingStatus := len(mappedMastersByRole[intenralhost.HostStatusInstalling]) +
		len(mappedMastersByRole[intenralhost.HostStatusInstallingInProgress]) +
		len(mappedMastersByRole[intenralhost.HostStatusInstalled]) +
		len(mappedMastersByRole[intenralhost.HostStatusInstallingPendingUserAction])
	if mastersInSomeInstallingStatus >= minHostsNeededForInstallation {
		return clusterStatusInstalling, statusInfoInstalling, nil
	}

	// Cluster is in error
	log.Infof("Cluster %s hosts status map is %+v", c.ID, mappedMastersByRole)
	log.Warningf("Cluster %s has hosts in error.", c.ID)
	return clusterStatusError, fmt.Sprintf("cluster %s has hosts in error", c.ID), nil
}
