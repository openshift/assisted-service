package cluster

import (
	"github.com/filanov/stateswitch"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
)

type stateCluster struct {
	srcState string
	cluster  *common.Cluster
}

func newStateCluster(c *common.Cluster) *stateCluster {
	return &stateCluster{
		srcState: swag.StringValue(c.Status),
		cluster:  c,
	}
}

func (sh *stateCluster) State() stateswitch.State {
	return stateswitch.State(swag.StringValue(sh.cluster.Status))
}

func (sh *stateCluster) SetState(state stateswitch.State) error {
	sh.cluster.Status = swag.String(string(state))
	return nil
}
