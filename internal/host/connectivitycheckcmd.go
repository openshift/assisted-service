package host

import (
	"context"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/jinzhu/gorm"

	"github.com/filanov/bm-inventory/models"
)

type connectivityCheckCmd struct {
	baseCmd
	db *gorm.DB
}

func NewConnectivityCheckCmd(log logrus.FieldLogger, db *gorm.DB) *connectivityCheckCmd {
	return &connectivityCheckCmd{
		baseCmd: baseCmd{log: log},
		db:      db,
	}
}

func (c *connectivityCheckCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	step := &models.Step{}
	step.StepType = models.StepTypeConnectivityCheck
	step.Command = "podman"

	var hosts []*models.Host
	if err := c.db.Find(&hosts, "cluster_id = ?", host.ClusterID).Error; err != nil {
		c.log.WithError(err).Errorf("failed to get list of hosts for cluster %s", host.ClusterID)
		return nil, err
	}
	//TODO need to verify with Ori what is the extra args for connectivity check
	// for i := range hosts {

	// }

	step.Args = strings.Split("run,--rm,--privileged,--quiet,--net=host,-v,/var/log:/var/log,quay.io/oamizur/connectivity_check,/usr/bin/connectivity_check", ",")
	return step, nil
}
