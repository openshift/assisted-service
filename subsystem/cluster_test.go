package subsystem

import (
	"context"

	"github.com/go-openapi/strfmt"

	"github.com/filanov/bm-inventory/client/inventory"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster tests", func() {
	ctx := context.Background()
	var cluster *inventory.RegisterClusterCreated
	var clusterID strfmt.UUID
	var err error
	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		cluster, err = bmclient.Inventory.RegisterCluster(ctx, &inventory.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name: swag.String("test cluster"),
			},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		clusterID = *cluster.GetPayload().ID
	})

	It("cluster CRUD", func() {
		_ = registerHost(clusterID)
		Expect(err).NotTo(HaveOccurred())

		getReply, err := bmclient.Inventory.GetCluster(ctx, &inventory.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		list, err := bmclient.Inventory.ListClusters(ctx, &inventory.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = bmclient.Inventory.DeregisterCluster(ctx, &inventory.DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		list, err = bmclient.Inventory.ListClusters(ctx, &inventory.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = bmclient.Inventory.GetCluster(ctx, &inventory.GetClusterParams{ClusterID: clusterID})
		Expect(err).Should(HaveOccurred())
	})

	It("cluster update", func() {
		host1 := registerHost(clusterID)
		host2 := registerHost(clusterID)

		publicKey := "my-public-key"

		c, err := bmclient.Inventory.UpdateCluster(ctx, &inventory.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				SSHPublicKey: publicKey,
				HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
					{
						ID:   *host1.ID,
						Role: "master",
					},
					{
						ID:   *host2.ID,
						Role: "worker",
					},
				},
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(c.GetPayload().SSHPublicKey).Should(Equal(publicKey))

		h := getHost(clusterID, *host1.ID)
		Expect(h.Role).Should(Equal("master"))

		h = getHost(clusterID, *host2.ID)
		Expect(h.Role).Should(Equal("worker"))
	})
})
