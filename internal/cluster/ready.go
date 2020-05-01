package cluster

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
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
	clusterIsReady, err := isClusterReady(c, db, r.log)
	if err != nil {
		return nil, errors.Errorf("unable to determine cluster %s hosts state ", c.ID)
	}

	if clusterIsReady {

		return &UpdateReply{
			State:     clusterStatusReady,
			IsChanged: false,
		}, nil
	} else {
		return updateState(clusterStatusInsufficient, c, db, r.log)

	}
}

func (r *readyState) Install(ctx context.Context, c *models.Cluster) (*UpdateReply, error) {
	return updateState(clusterStatusInstalling, c, r.db, r.log)
}

func (r *readyState) DeregisterCluster(ctx context.Context, c *models.Cluster) (*UpdateReply, error) {
	return deregisterCluster(c, r.db)
}
