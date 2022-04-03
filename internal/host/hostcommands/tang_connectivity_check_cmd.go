package hostcommands

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type tangConnectivityCheckCmd struct {
	baseCmd
	db                         *gorm.DB
	tangConnectivityCheckImage string
}

func NewTangConnectivityCheckCmd(log logrus.FieldLogger, db *gorm.DB, tangConnectivityCheckImage string) *tangConnectivityCheckCmd {
	return &tangConnectivityCheckCmd{
		baseCmd:                    baseCmd{log: log},
		db:                         db,
		tangConnectivityCheckImage: tangConnectivityCheckImage,
	}
}

func (c *tangConnectivityCheckCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	var cluster common.Cluster

	if err := c.db.First(&cluster, "id = ?", host.ClusterID).Error; err != nil {
		c.log.WithError(err).Errorf("failed to fetch cluster %s", host.ClusterID)
		return nil, err
	}

	// Skip tangConnectivityCheck for cases where:
	// 1. DiskEncryption not set or not enabled.
	// 2. DiskEncryption mode is not tang based.
	// 3. DiskEncryption is not enabled, for the host role.
	if cluster.DiskEncryption == nil ||
		swag.StringValue(cluster.DiskEncryption.EnableOn) == models.DiskEncryptionEnableOnNone ||
		swag.StringValue(cluster.DiskEncryption.Mode) == models.DiskEncryptionModeTpmv2 ||
		!hostutil.IsDiskEncryptionEnabledForRole(*cluster.DiskEncryption, common.GetEffectiveRole(host)) {
		c.log.Debugf("skipping tangConnectivityCheck for host %s, DiskEncryption config do not require validation here", host.ID.String())
		return []*models.Step{}, nil
	}

	request := models.TangConnectivityRequest{TangServers: &cluster.DiskEncryption.TangServers}
	tangServers, err := json.Marshal(request)
	if err != nil {
		c.log.WithError(err).Errorf("failed to Marshal TangConnectivityRequest with for cluster %s", host.ClusterID)
		return nil, err
	}

	step := &models.Step{
		StepType: models.StepTypeTangConnectivityCheck,
		Args: []string{
			string(tangServers),
		},
	}
	return []*models.Step{step}, nil
}
