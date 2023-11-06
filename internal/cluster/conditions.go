package cluster

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
)

type conditionId string
type condition struct {
	id conditionId
	fn func(c *clusterPreprocessContext) bool
}

const (
	VipDhcpAllocationSet         = conditionId("vip-dhcp-allocation-set")
	AllHostsPreparedSuccessfully = conditionId("all-hosts-prepared-successfully")
	UnPreparingtHostsExist       = conditionId("unpreparing-hosts-exist")
	FailedPreparingtHostsExist   = conditionId("failed-preparing-hosts-exist")
	ClusterPreparationSucceeded  = conditionId("cluster-preparation-succeeded")
	ClusterPreparationFailed     = conditionId("cluster-preparation-failed")
)

func (c conditionId) String() string {
	return string(c)
}

func (v *clusterValidator) isVipDhcpAllocationSet(c *clusterPreprocessContext) bool {
	return swag.BoolValue(c.cluster.VipDhcpAllocation)
}

func (v *clusterValidator) areAllHostsPreparedSuccessfully(c *clusterPreprocessContext) bool {
	for _, h := range c.cluster.Hosts {
		if swag.StringValue(h.Status) != models.HostStatusPreparingSuccessful {
			return false
		}
	}
	return true
}

func (v *clusterValidator) isUnPreparingHostsExist(c *clusterPreprocessContext) bool {
	validStates := []string{
		models.HostStatusPreparingForInstallation,
		models.HostStatusPreparingSuccessful,
		models.HostStatusPreparingFailed,
		models.HostStatusKnown,
	}
	for _, h := range c.cluster.Hosts {
		if !funk.ContainsString(validStates, swag.StringValue(h.Status)) {
			return true
		}
	}
	return false
}

func (v *clusterValidator) isFailedPreparingHostExist(c *clusterPreprocessContext) bool {
	for _, h := range c.cluster.Hosts {
		if models.HostStatusPreparingFailed == swag.StringValue(h.Status) {
			return true
		}
	}
	return false
}

func (v *clusterValidator) isClusterPreparationSucceeded(c *clusterPreprocessContext) bool {
	return c.cluster.LastInstallationPreparation.Status == models.LastInstallationPreparationStatusSuccess
}

func (v *clusterValidator) isClusterPreparationFailed(c *clusterPreprocessContext) bool {
	return c.cluster.LastInstallationPreparation.Status == models.LastInstallationPreparationStatusFailed
}
