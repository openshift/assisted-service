package cluster

import (
	context "context"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
)

func NewErrorState(log logrus.FieldLogger, db *gorm.DB) *errorState {
	return &errorState{
		log: log,
		db:  db,
	}
}

type errorState baseState

func (e *errorState) RefreshStatus(ctx context.Context, c *models.Cluster, db *gorm.DB) (*UpdateReply, error) {
	return &UpdateReply{
		State:     clusterStatusError,
		IsChanged: false,
	}, nil
}

func (e *errorState) Install(ctx context.Context, c *models.Cluster) (*UpdateReply, error) {
	return nil, errors.Errorf("unable to install cluster <%s> in <%s> status",
		c.ID, swag.StringValue(c.Status))
}

func (e *errorState) DeregisterCluster(ctx context.Context, c *models.Cluster) (*UpdateReply, error) {
	// TODO is this true ?
	return nil, errors.Errorf("unable to deregister cluster <%s> in <%s> status",
		c.ID, swag.StringValue(c.Status))
}
