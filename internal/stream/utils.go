package stream

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	pkgstream "github.com/openshift/assisted-service/pkg/stream"
)

// NotifiableCluster wraps common.Cluster and provides event-specific
// transformations for notification payloads.
type NotifiableCluster struct {
	*common.Cluster
}

// Payload returns the event payload with observability fields included.
// This overrides the common.Cluster.Payload() method.
func (nc *NotifiableCluster) Payload() any {
	payload := &pkgstream.ClusterPayload{
		Cluster: nc.Cluster.Cluster,
	}

	if nc.Cluster.PrimaryIPStack != nil {
		payload.PrimaryIPStack = int(*nc.Cluster.PrimaryIPStack)
	}

	return payload
}

func GetNotifiableCluster(cluster *common.Cluster) *NotifiableCluster {
	// notify smaller cluster object. We already notify hosts updates that could become
	// problematic in bigger clusters, because the underlying max size of a message.
	clusterCopy := *cluster
	clusterCopy.Hosts = []*models.Host{}
	return &NotifiableCluster{Cluster: &clusterCopy}
}
