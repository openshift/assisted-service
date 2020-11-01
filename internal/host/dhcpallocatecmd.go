package host

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/sirupsen/logrus"

	"github.com/openshift/assisted-service/models"
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
	nic, err := network.GetMachineCIDRInterface(host, cluster)
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
	var cluster common.Cluster
	if err := f.db.Take(&cluster, "id = ?", host.ClusterID.String()).Error; err != nil {
		return nil, err
	}
	/*
	 * Since this function may be invoked in case that DHCP allocate command should not be sent to the host
	 * filtering is done here to remove all valid (not errored) cases that the command should not be invoked.
	 * These cases are:
	 * - VipDhcpAllocation is false: DHCP mode is not enabled
	 * - MachineNetworkCidr is empty: MachineNetworkCidr has not be set by the user
	 * - Inventory is empty: Inventory has not been received yet from the host
	 */
	if !swag.BoolValue(cluster.VipDhcpAllocation) || cluster.MachineNetworkCidr == "" || host.Inventory == "" {
		return nil, nil
	}
	param, err := f.prepareParam(host, &cluster)
	if err != nil {
		return nil, err
	}
	step := &models.Step{
		StepType: models.StepTypeDhcpLeaseAllocate,
		Command:  "podman",
		Args: []string{
			"run", "--privileged", "--net=host", "--rm", "--quiet",
			"-v", "/var/log:/var/log",
			"-v", "/run/systemd/journal/socket:/run/systemd/journal/socket",
			f.dhcpAllocateImage,
			"dhcp_lease_allocate",
			param,
		},
	}
	return []*models.Step{step}, nil
}
