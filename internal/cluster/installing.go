package cluster

import (
	context "context"
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/models"

	"github.com/openshift/assisted-service/internal/common"
	intenralhost "github.com/openshift/assisted-service/internal/host"

	logutil "github.com/openshift/assisted-service/pkg/log"

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

	mappedMastersByRole := MapMasterHostsByStatus(c)

	// Cluster is in finalizing
	mastersInInstalled, ok := mappedMastersByRole[intenralhost.HostStatusInstalled]
	if ok && len(mastersInInstalled) >= MinHostsNeededForInstallation {
		log.Infof("Cluster %s has at least %d installed hosts, cluster is installed.", c.ID, len(mastersInInstalled))
		return models.ClusterStatusFinalizing, statusInfoFinalizing, nil
	}

	// Cluster is installing
	mastersInSomeInstallingStatus := len(mappedMastersByRole[intenralhost.HostStatusInstalling]) +
		len(mappedMastersByRole[intenralhost.HostStatusInstallingInProgress]) +
		len(mappedMastersByRole[intenralhost.HostStatusInstalled]) +
		len(mappedMastersByRole[intenralhost.HostStatusInstallingPendingUserAction])
	if mastersInSomeInstallingStatus >= MinHostsNeededForInstallation {
		return clusterStatusInstalling, statusInfoInstalling, nil
	}

	// Cluster is in error
	mappedHostsRolesToIds := make(map[string][]strfmt.UUID, len(mappedMastersByRole))
	for role, hosts := range mappedMastersByRole {
		ids := make([]strfmt.UUID, 0)
		for _, h := range hosts {
			ids = append(ids, *h.ID)
		}

		mappedHostsRolesToIds[role] = ids
	}

	log.Warningf("Cluster %s hosts status map is %+v", c.ID, mappedHostsRolesToIds)
	return clusterStatusError, fmt.Sprintf("cluster %s has hosts in error", c.ID), nil
}
