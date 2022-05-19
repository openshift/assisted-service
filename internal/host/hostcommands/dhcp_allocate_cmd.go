package hostcommands

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type dhcpAllocateCmd struct {
	baseCmd
	dhcpAllocateImage string
	db                *gorm.DB
}

func NewDhcpAllocateCmd(log logrus.FieldLogger, dhcpAllocateImage string, db *gorm.DB) *dhcpAllocateCmd {
	return &dhcpAllocateCmd{
		baseCmd:           baseCmd{log: log},
		dhcpAllocateImage: dhcpAllocateImage,
		db:                db,
	}
}

func asMAC(macStr string) *strfmt.MAC {
	mac := strfmt.MAC(macStr)
	return &mac
}

func (f *dhcpAllocateCmd) prepareParam(host *models.Host, cluster *common.Cluster) (string, error) {
	nic, err := network.GetPrimaryMachineCIDRInterface(host, cluster)
	if err != nil {
		return "", err
	}
	clusterID := host.ClusterID.String()

	request := models.DhcpAllocationRequest{
		APIVipMac:       asMAC(network.GenerateAPIVipMAC(clusterID)),
		IngressVipMac:   asMAC(network.GenerateIngressVipMAC(clusterID)),
		APIVipLease:     cluster.ApiVipLease,
		IngressVipLease: cluster.IngressVipLease,
		Interface:       swag.String(nic),
	}
	b, err := json.Marshal(&request)
	if err != nil {
		f.log.WithError(err).Warn("Json marshal")
		return "", err
	}
	return string(b), nil
}

func (f *dhcpAllocateCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	cluster := &common.Cluster{}
	if err := f.db.Preload(common.MachineNetworksTable).Select("id", "vip_dhcp_allocation", "api_vip_lease", "ingress_vip_lease").
		Take(cluster, "id = ?", host.ClusterID.String()).Error; err != nil {
		return nil, err
	}

	/*
	 * Since this function may be invoked in case that DHCP allocate command should not be sent to the host
	 * filtering is done here to remove all valid (not errored) cases that the command should not be invoked.
	 * These cases are:
	 * - VipDhcpAllocation is false: DHCP mode is not enabled
	 * - MachineNetworks is empty: Machine Network Cidr has not been set by the user
	 * - Inventory is empty: Inventory has not been received yet from the host
	 */
	if !swag.BoolValue(cluster.VipDhcpAllocation) || !network.IsMachineCidrAvailable(cluster) || host.Inventory == "" {
		return nil, nil
	}
	param, err := f.prepareParam(host, cluster)
	if err != nil {
		return nil, err
	}
	step := &models.Step{
		StepType: models.StepTypeDhcpLeaseAllocate,
		Args: []string{
			param,
		},
	}
	return []*models.Step{step}, nil
}
