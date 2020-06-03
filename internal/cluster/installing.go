package cluster

import (
	context "context"
	"fmt"

	intenralhost "github.com/filanov/bm-inventory/internal/host"

	logutil "github.com/filanov/bm-inventory/pkg/log"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
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

func (i *installingState) RefreshStatus(ctx context.Context, c *models.Cluster, db *gorm.DB) (*UpdateReply, error) {
	log := logutil.FromContext(ctx, i.log)
	installationState, StateInfo, err := i.getClusterInstallationState(ctx, c, db)
	if err != nil {
		return nil, errors.Errorf("couldn't determine cluster %s installation state", c.ID)
	}

	switch installationState {
	case clusterStatusInstalled:
		return updateState(clusterStatusInstalled, StateInfo, c, db, log)
	case clusterStatusError:
		return updateState(clusterStatusError, StateInfo, c, db, log)
	case clusterStatusInstalling:
		return &UpdateReply{
			State:     clusterStatusInstalling,
			IsChanged: false,
		}, nil
	}
	return nil, errors.Errorf("cluster % state transaction is not clear, installation state: %s ", c.ID, installationState)
}

func (i *installingState) getClusterInstallationState(ctx context.Context, c *models.Cluster, db *gorm.DB) (string, string, error) {
	log := logutil.FromContext(ctx, i.log)

	if err := db.Preload("Hosts").First(&c, "id = ?", c.ID).Error; err != nil {
		return "", "", errors.Errorf("cluster %s not found", c.ID)
	}

	mappedMastersByRole := mapMasterHostsByStatus(c)

	// Cluster is in installed
	mastersInInstalled, ok := mappedMastersByRole[intenralhost.HostStatusInstalled]
	if ok && len(mastersInInstalled) >= minHostsNeededForInstallation {
		log.Infof("Cluster %s has at least %d installed hosts, cluster is installed.", c.ID, len(mastersInInstalled))
		return clusterStatusInstalled, statusInfoInstalled, nil
	}

	// Cluster is installing
	mastersInInstalling := mappedMastersByRole[intenralhost.HostStatusInstalling]
	mastersInInstallingInProgress := mappedMastersByRole[intenralhost.HostStatusInstallingInProgress]
	if (len(mastersInInstalling) + len(mastersInInstallingInProgress) + len(mastersInInstalled)) >= minHostsNeededForInstallation {
		return clusterStatusInstalling, statusInfoInstalling, nil
	}

	// Cluster is in error
	mastersInError := mappedMastersByRole[intenralhost.HostStatusError]
	log.Warningf("Cluster %s has %d hosts in error.", c.ID, len(mastersInError))
	return clusterStatusError, fmt.Sprintf("cluster %s has %d hosts in error", c.ID, len(mastersInError)), nil
}
