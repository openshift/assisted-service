package cluster

import (
	context "context"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/sirupsen/logrus"

	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
)

func NewInstalledState(log logrus.FieldLogger, db *gorm.DB) *installedState {
	return &installedState{
		log: log,
		db:  db,
	}
}

type installedState baseState

var _ StateAPI = (*Manager)(nil)

func (i *installedState) RefreshStatus(ctx context.Context, c *common.Cluster, db *gorm.DB) (*common.Cluster, error) {
	return c, nil
}

func (i *installedState) Install(ctx context.Context, c *common.Cluster) error {
	return errors.Errorf("unable to install cluster <%s> in <%s> status",
		c.ID, swag.StringValue(c.Status))
}
