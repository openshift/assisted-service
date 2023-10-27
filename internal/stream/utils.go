package stream

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

func GetNotifiableCluster(cluster *common.Cluster) *common.Cluster {
	// notify smaller cluster object. We already notify hosts updates that could become
	// problematic in bigger clusters, because the underlying max size of a message
	notifiableCluster := *cluster
	notifiableCluster.Hosts = []*models.Host{}
	return &notifiableCluster
}
