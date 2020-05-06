package cluster

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
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

func (i *insufficientState) RefreshStatus(ctx context.Context, c *models.Cluster, db *gorm.DB) (*UpdateReply, error) {

	clusterIsReady, err := isClusterReady(c, db, i.log)
	if err != nil {
		return nil, errors.Errorf("unable to determine cluster %s hosts state ", c.ID)
	}

	if clusterIsReady {
		return updateState(clusterStatusReady, c, db, i.log)
	} else {
		i.log.Infof("Cluster %s does not have sufficient resources to be installed.", c.ID)
		return &UpdateReply{
			State:     clusterStatusInsufficient,
			IsChanged: false,
		}, nil
	}
}

func (i *insufficientState) Install(ctx context.Context, c *models.Cluster) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to install cluster <%s> in <%s> status",
		c.ID, swag.StringValue(c.Status))
}
