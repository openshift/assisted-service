package host

import (
	"context"

	"github.com/filanov/bm-inventory/internal/connectivity"

	"github.com/sirupsen/logrus"

	"github.com/jinzhu/gorm"

	"github.com/filanov/bm-inventory/models"
)

type connectivityCheckCmd struct {
	baseCmd
	db                     *gorm.DB
	connectivityValidator  connectivity.Validator
	connectivityCheckImage string
}

func NewConnectivityCheckCmd(log logrus.FieldLogger, db *gorm.DB, connectivityValidator connectivity.Validator, connectivityCheckImage string) *connectivityCheckCmd {
	return &connectivityCheckCmd{
		baseCmd:                baseCmd{log: log},
		db:                     db,
		connectivityValidator:  connectivityValidator,
		connectivityCheckImage: connectivityCheckImage,
	}
}

func (c *connectivityCheckCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {

	var hosts []*models.Host
	if err := c.db.Find(&hosts, "cluster_id = ?", host.ClusterID).Error; err != nil {
		c.log.WithError(err).Errorf("failed to get list of hosts for cluster %s", host.ClusterID)
		return nil, err
	}

	hostsData, err := convertHostsToConnectivityCheckParams(host.ID, hosts, c.connectivityValidator)
	if err != nil {
		c.log.Errorf("failed to convert hosts to connectivity params for host %s cluster %s", host.ID, host.ClusterID)
		return nil, err
	}

	step := &models.Step{
		StepType: models.StepTypeConnectivityCheck,
		Command:  "podman",
		Args: []string{
			"run", "--privileged", "--net=host", "--rm", "--quiet",
			"-v", "/var/log:/var/log",
			"-v", "/run/systemd/journal/socket:/run/systemd/journal/socket",
			c.connectivityCheckImage,
			"connectivity_check",
			hostsData,
		},
	}
	return step, nil
}
