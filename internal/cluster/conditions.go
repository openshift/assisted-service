package cluster

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

type conditionId string
type condition struct {
	id conditionId
	fn func(c *clusterPreprocessContext) bool
}

const (
	VipDhcpAllocationSet         = conditionId("vip-dhcp-allocation-set")
	AllHostsPreparedSuccessfully = conditionId("all-hosts-prepared-successfully")
	InsufficientHostExists       = conditionId("insufficient-host-exists")
	ClusterPreparationSucceeded  = conditionId("cluster-preparation-succeeded")
	ClusterPreparationFailed     = conditionId("cluster-preparation-failed")
)

func (c conditionId) String() string {
	return string(c)
}

func isVipDhcpAllocationSet(c *clusterPreprocessContext) bool {
	return swag.BoolValue(c.cluster.VipDhcpAllocation)
}

func areAllHostsPreparedSuccessfully(c *clusterPreprocessContext) bool {
	for _, h := range c.cluster.Hosts {
		if swag.StringValue(h.Status) != models.HostStatusPreparingSuccessful {
			return false
		}
	}
	return true
}

func isInsufficientHostExists(c *clusterPreprocessContext) bool {
	for _, h := range c.cluster.Hosts {
		if swag.StringValue(h.Status) == models.HostStatusInsufficient {
			return true
		}
	}
	return false
}

func isClusterPreparationSucceeded(c *clusterPreprocessContext) bool {
	return c.cluster.InstallationPreparationCompletionStatus == common.InstallationPreparationSucceeded
}

func isClusterPreparationFailed(c *clusterPreprocessContext) bool {
	return c.cluster.InstallationPreparationCompletionStatus == common.InstallationPreparationFailed
}
