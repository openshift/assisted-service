package stream_test

import (
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/stream"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Get notifiable cluster", func() {
	It("should remove hosts field", func() {
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		clusterModel := models.Cluster{
			ID: &clusterId,
			Hosts: []*models.Host{
				{
					ID: &id,
				},
			},
		}
		cluster := &common.Cluster{
			Cluster: clusterModel,
		}
		notifiableCluster := stream.GetNotifiableCluster(cluster)
		Expect(notifiableCluster).ShouldNot(Equal(cluster))

		clusterModelWithEmptyHosts := models.Cluster{
			ID:    &clusterId,
			Hosts: []*models.Host{},
		}
		clusterWithEmptyHosts := &common.Cluster{
			Cluster: clusterModelWithEmptyHosts,
		}
		Expect(notifiableCluster).Should(Equal(clusterWithEmptyHosts))
	})
})
