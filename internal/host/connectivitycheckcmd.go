package host

import (
	"context"

	"github.com/filanov/bm-inventory/internal/hardware"

	"github.com/sirupsen/logrus"

	"github.com/jinzhu/gorm"

	"github.com/filanov/bm-inventory/models"
)

type connectivityCheckCmd struct {
	baseCmd
	db          *gorm.DB
	hwValidator hardware.Validator
}

func NewConnectivityCheckCmd(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator) *connectivityCheckCmd {
	return &connectivityCheckCmd{
		baseCmd:     baseCmd{log: log},
		db:          db,
		hwValidator: hwValidator,
	}
}

func (c *connectivityCheckCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	step := &models.Step{}
	step.StepType = models.StepTypeConnectivityCheck

	var hosts []*models.Host
	if err := c.db.Find(&hosts, "cluster_id = ?", host.ClusterID).Error; err != nil {
		c.log.WithError(err).Errorf("failed to get list of hosts for cluster %s", host.ClusterID)
		return nil, err
	}

	hostsData, err := convertHostsToConnectivityCheckParams(host.ID, hosts, c.hwValidator)
	if err != nil {
		c.log.Errorf("failed to convert hosts to connectivity params for host %s cluster %s", host.ID, host.ClusterID)
		return nil, err
	}

	step.Args = append(step.Args, hostsData)
	return step, nil
}
