package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
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

	ignitionEndpointUrl, err := getIngnitionEndPoint(&cluster, host)
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

	if commonHost.IgnitionEndpointToken != "" {
		request.IgnitionEndpointToken = &commonHost.IgnitionEndpointToken
	}

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

func getIngnitionEndPoint(cluster *common.Cluster, host *models.Host) (string, error) {
	addressPart := swag.StringValue(cluster.APIVipDNSName)
	if addressPart == "" {
		addressPart = cluster.APIVip
	}
	poolName := string(common.GetEffectiveRole(host))
	if host.MachineConfigPoolName != "" {
		poolName = host.MachineConfigPoolName
	}
	ignitionEndpointUrl := fmt.Sprintf("http://%s:22624/config/%s", addressPart, poolName)
	if cluster.IgnitionEndpoint != nil && cluster.IgnitionEndpoint.URL != nil {
		url, err := url.Parse(*cluster.IgnitionEndpoint.URL)
		if err != nil {
			return "", err
		}
		url.Path = path.Join(url.Path, poolName)
		ignitionEndpointUrl = url.String()
	}
	return ignitionEndpointUrl, nil
}
