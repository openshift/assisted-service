package cluster

import (
	context "context"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
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
	return &UpdateReply{
		State:     clusterStatusInstalling,
		IsChanged: false,
	}, nil
}

func (i *installingState) Install(ctx context.Context, c *models.Cluster) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to install cluster <%s> in <%s> status",
		c.ID, swag.StringValue(c.Status))
}

func (i *installingState) DeregisterCluster(ctx context.Context, c *models.Cluster) (*UpdateReply, error) {
	// TODO is this true ?
	return nil, errors.Errorf("unable to deregister cluster <%s> in <%s> status",
		c.ID, swag.StringValue(c.Status))
}
