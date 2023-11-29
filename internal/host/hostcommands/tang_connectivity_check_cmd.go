package hostcommands

import (
	"context"
	"encoding/json"

	ignition_types "github.com/coreos/ignition/v2/config/v3_2/types"
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

func (c *tangConnectivityCheckCmd) getTangServersFromHostIgnition(host *models.Host) ([]byte, error) {
	var (
		luks        *ignition_types.Luks
		tangServers []byte
		err         error
	)
	if host.APIVipConnectivity != "" {
		luks, err = hostutil.GetDiskEncryptionForDay2(c.log, host)
		if err != nil {
			return nil, err
		}
		if len(luks.Clevis.Tang) != 0 {
			if tangServers, err = json.Marshal(luks.Clevis.Tang); err != nil {
				return nil, err
			}
			serverList := string(tangServers[:])
			request, err := json.Marshal(
				models.TangConnectivityRequest{
					TangServers: &serverList,
				},
			)
			if err != nil {
				return nil, err
			}
			return request, nil
		}
	}
	return nil, nil
}

func (c *tangConnectivityCheckCmd) shouldRunTangConnectivityCheck(cluster common.Cluster, host *models.Host) bool {
	// Skip tangConnectivityCheck for cases where:
	// 1. DiskEncryption not set or not enabled.
	// 2. DiskEncryption mode is not tang based.
	// 3. DiskEncryption is not enabled, for the host role.
	if cluster.DiskEncryption == nil ||
		swag.StringValue(cluster.DiskEncryption.EnableOn) == models.DiskEncryptionEnableOnNone ||
		swag.StringValue(cluster.DiskEncryption.Mode) == models.DiskEncryptionModeTpmv2 ||
		!hostutil.IsDiskEncryptionEnabledForRole(*cluster.DiskEncryption, common.GetEffectiveRole(host)) {
		c.log.Debugf("skipping tangConnectivityCheck for host %s, cluster DiskEncryption config does not require validation here",
			host.ID.String())
		return false
	}
	return true
}

func (c *tangConnectivityCheckCmd) getTangServersFromCluster(cluster common.Cluster, host *models.Host) ([]byte, error) {
	var (
		tangServers []byte
		err         error
	)
	request := models.TangConnectivityRequest{TangServers: &cluster.DiskEncryption.TangServers}
	tangServers, err = json.Marshal(request)
	if err != nil {
		c.log.WithError(err).Errorf("failed to Marshal TangConnectivityRequest with for cluster %s", host.ClusterID)
		return nil, err
	}
	return tangServers, nil
}

func (c *tangConnectivityCheckCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	var (
		cluster     common.Cluster
		tangServers []byte
		err         error
	)

	if err = c.db.First(&cluster, "id = ?", host.ClusterID).Error; err != nil {
		c.log.WithError(err).Errorf("failed to fetch cluster %s", host.ClusterID)
		return nil, err
	}
	if swag.BoolValue(cluster.Imported) { // Day 2 imported cluster
		// In this case, it is assumed that the cluster was imported, and thus the DB will have no information
		// about DiskEncryption settings. Therefore, try to read those settings from the host ignition.
		if tangServers, err = c.getTangServersFromHostIgnition(host); err != nil {
			return nil, err
		}
		if tangServers == nil {
			c.log.Debugf(
				"skipping tangConnectivityCheck for host %s, host ignition DiskEncryption config do not require validation here",
				host.ID.String())
			return []*models.Step{}, nil
		}
	} else { // Day 1
		if !c.shouldRunTangConnectivityCheck(cluster, host) {
			return []*models.Step{}, nil
		}
		if tangServers, err = c.getTangServersFromCluster(cluster, host); err != nil {
			return nil, err
		}
	}
	if err != nil {
		c.log.Errorf("Could not create tang connectivity request from tang servers in host due to error: %s", err.Error())
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
