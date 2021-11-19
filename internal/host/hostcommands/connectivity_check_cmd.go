package hostcommands

import (
	"context"

	"github.com/openshift/assisted-service/internal/connectivity"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
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

func (c *connectivityCheckCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {

	var hosts []*models.Host
	if err := c.db.Find(&hosts, "cluster_id = ?", host.ClusterID).Error; err != nil {
		c.log.WithError(err).Errorf("failed to get list of hosts for cluster %s", host.ClusterID)
		return nil, err
	}

	hostsData, err := convertHostsToConnectivityCheckParams(host.ID, hosts, c.connectivityValidator)
	if err != nil {
		c.log.WithError(err).Errorf("failed to convert hosts to connectivity params for host %s cluster %s", host.ID, host.ClusterID)
		return nil, err
	}

	// Skip this step in case there is no hosts to check
	if hostsData == "" {
		return nil, nil
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
	return []*models.Step{step}, nil
}
