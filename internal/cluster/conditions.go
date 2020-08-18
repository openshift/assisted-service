package cluster

import "github.com/go-openapi/swag"

type conditionId string
type condition struct {
	id conditionId
	fn func(c *clusterPreprocessContext) bool
}

const (
	VipDhcpAllocationSet = conditionId("vip-dhcp-allocation-set")
)

func (c conditionId) String() string {
	return string(c)
}

func isVipDhcpAllocationSet(c *clusterPreprocessContext) bool {
	return swag.BoolValue(c.cluster.VipDhcpAllocation)
}
