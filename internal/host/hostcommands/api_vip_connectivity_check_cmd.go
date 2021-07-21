package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	models "github.com/openshift/assisted-service/models/v1"
	"github.com/sirupsen/logrus"
)

type apivipConnectivityCheckCmd struct {
	baseCmd
	db                     *gorm.DB
	connectivityCheckImage string
	verifyAPIVipCidr       bool
}

func NewAPIVIPConnectivityCheckCmd(log logrus.FieldLogger, db *gorm.DB, connectivityCheckImage string, verifyAPIVipCidr bool) *apivipConnectivityCheckCmd {
	return &apivipConnectivityCheckCmd{
		baseCmd:                baseCmd{log: log},
		db:                     db,
		connectivityCheckImage: connectivityCheckImage,
		verifyAPIVipCidr:       verifyAPIVipCidr,
	}
}

func (c *apivipConnectivityCheckCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	var cluster common.Cluster
	if err := c.db.First(&cluster, "id = ?", host.ClusterID).Error; err != nil {
		c.log.WithError(err).Errorf("failed to fetch cluster %s", host.ClusterID)
		return nil, err
	}

	addressPart := swag.StringValue(cluster.APIVipDNSName)
	if addressPart == "" {
		addressPart = cluster.APIVip
	}

	apiURL := fmt.Sprintf("http://%s:22624/config/worker", addressPart)
	request := models.APIVipConnectivityRequest{
		URL:        &apiURL,
		VerifyCidr: c.verifyAPIVipCidr,
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		c.log.WithError(err).Errorf("failed to marshal APIVipConnectivityRequest")
		return nil, err
	}

	step := &models.Step{
		StepType: models.StepTypeAPIVipConnectivityCheck,
		Command:  "podman",
		Args: []string{
			"run", "--privileged", "--net=host", "--rm", "--quiet",
			"-v", "/var/log:/var/log",
			"-v", "/run/systemd/journal/socket:/run/systemd/journal/socket",
			c.connectivityCheckImage,
			"apivip_check",
			string(requestBytes),
		},
	}
	return []*models.Step{step}, nil
}
