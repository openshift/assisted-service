package subsystem

import (
	"context"

	"github.com/filanov/bm-inventory/client/inventory"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster tests", func() {
	ctx := context.Background()
	AfterEach(func() {
		clearDB()
	})

	It("cluster CRUD", func() {
		host, err := bmclient.Inventory.RegisterHost(ctx, &inventory.RegisterHostParams{
			NewHostParams: &models.HostCreateParams{
				HostID:    strToUUID(uuid.New().String()),
				Namespace: swag.String("my namespace"),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		cluster, err := bmclient.Inventory.RegisterCluster(ctx, &inventory.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Description: "my cluster",
				Name:        swag.String("test cluster"),
				Hosts: []*models.ClusterCreateParamsHostsItems0{
					{
						ID:   *host.GetPayload().ID,
						Role: "master",
					},
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		getReply, err := bmclient.Inventory.GetCluster(ctx, &inventory.GetClusterParams{ClusterID: cluster.GetPayload().ID.String()})
		Expect(err).NotTo(HaveOccurred())

		Expect(getReply.GetPayload().Hosts[0].ClusterID).Should(Equal(*cluster.GetPayload().ID))
		Expect(getReply.GetPayload().Hosts[0].Role).Should(Equal("master"))

		list, err := bmclient.Inventory.ListClusters(ctx, &inventory.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = bmclient.Inventory.DeregisterCluster(ctx, &inventory.DeregisterClusterParams{ClusterID: cluster.GetPayload().ID.String()})
		Expect(err).NotTo(HaveOccurred())

		list, err = bmclient.Inventory.ListClusters(ctx, &inventory.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = bmclient.Inventory.GetCluster(ctx, &inventory.GetClusterParams{ClusterID: cluster.GetPayload().ID.String()})
		Expect(err).Should(HaveOccurred())

	})
})
