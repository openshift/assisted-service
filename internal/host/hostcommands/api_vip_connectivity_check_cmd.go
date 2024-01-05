package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type apivipConnectivityCheckCmd struct {
	baseCmd
	db                     *gorm.DB
	connectivityCheckImage string
}

func NewAPIVIPConnectivityCheckCmd(log logrus.FieldLogger, db *gorm.DB, connectivityCheckImage string) *apivipConnectivityCheckCmd {
	return &apivipConnectivityCheckCmd{
		baseCmd:                baseCmd{log: log},
		db:                     db,
		connectivityCheckImage: connectivityCheckImage,
	}
}

func (c *apivipConnectivityCheckCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	var cluster common.Cluster
	if err := c.db.First(&cluster, "id = ?", host.ClusterID).Error; err != nil {
		c.log.WithError(err).Errorf("failed to fetch cluster %s", host.ClusterID)
		return nil, err
	}
	var commonHost common.Host
	if err := c.db.First(&commonHost, "id = ?", host.ID).Error; err != nil {
		c.log.WithError(err).Errorf("failed to fetch common Host %s", host.ID)
		return nil, err
	}

	ignitionEndpointUrl, err := hostutil.GetIgnitionEndpoint(&cluster, host)
	if err != nil {
		c.log.WithError(err).Errorf("failed to build Ignition Endpoint %s", host.ID)
		return nil, err
	}
	request := models.APIVipConnectivityRequest{
		URL: &ignitionEndpointUrl,
	}

	if cluster.IgnitionEndpoint != nil && cluster.IgnitionEndpoint.CaCertificate != nil {
		request.CaCertificate = cluster.IgnitionEndpoint.CaCertificate
	}

	var requestHeaders []*models.APIVipConnectivityAdditionalRequestHeader
	if commonHost.IgnitionEndpointToken != "" {
		requestHeaders = append(requestHeaders, &models.APIVipConnectivityAdditionalRequestHeader{Key: "Authorization", Value: fmt.Sprintf("Bearer %s", commonHost.IgnitionEndpointToken)})
		request.IgnitionEndpointToken = &commonHost.IgnitionEndpointToken
	}
	request.RequestHeaders = requestHeaders

	requestBytes, err := json.Marshal(request)
	if err != nil {
		c.log.WithError(err).Errorf("failed to marshal APIVipConnectivityRequest")
		return nil, err
	}

	step := &models.Step{
		StepType: models.StepTypeAPIVipConnectivityCheck,
		Args: []string{
			string(requestBytes),
		},
	}
	return []*models.Step{step}, nil
}
