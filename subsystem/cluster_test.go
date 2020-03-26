package subsystem

import (
	"context"

	"github.com/go-openapi/strfmt"

	"github.com/filanov/bm-inventory/client/inventory"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster tests", func() {
	ctx := context.Background()
	var cluster *inventory.RegisterClusterCreated
	var clusterID *strfmt.UUID
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
		clusterID = cluster.GetPayload().ID
	})

	It("cluster CRUD", func() {
		_, err = bmclient.Inventory.RegisterHost(ctx, &inventory.RegisterHostParams{
			NewHostParams: &models.HostCreateParams{
				HostID:    strToUUID(uuid.New().String()),
				ClusterID: clusterID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		getReply, err := bmclient.Inventory.GetCluster(ctx, &inventory.GetClusterParams{ClusterID: clusterID.String()})
		Expect(err).NotTo(HaveOccurred())

		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		list, err := bmclient.Inventory.ListClusters(ctx, &inventory.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = bmclient.Inventory.DeregisterCluster(ctx, &inventory.DeregisterClusterParams{ClusterID: clusterID.String()})
		Expect(err).NotTo(HaveOccurred())

		list, err = bmclient.Inventory.ListClusters(ctx, &inventory.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = bmclient.Inventory.GetCluster(ctx, &inventory.GetClusterParams{ClusterID: clusterID.String()})
		Expect(err).Should(HaveOccurred())
	})

	It("cluster update", func() {
		host1, err := bmclient.Inventory.RegisterHost(ctx, &inventory.RegisterHostParams{
			NewHostParams: &models.HostCreateParams{
				HostID:    strToUUID(uuid.New().String()),
				ClusterID: clusterID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		host2, err := bmclient.Inventory.RegisterHost(ctx, &inventory.RegisterHostParams{
			NewHostParams: &models.HostCreateParams{
				HostID:    strToUUID(uuid.New().String()),
				ClusterID: clusterID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		publicKey := "my-public-key"

		c, err := bmclient.Inventory.UpdateCluster(ctx, &inventory.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				SSHPublicKey: publicKey,
				HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
					{
						ID:   *host1.GetPayload().ID,
						Role: "master",
					},
					{
						ID:   *host2.GetPayload().ID,
						Role: "worker",
					},
				},
			},
			ClusterID: clusterID.String(),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(c.GetPayload().SSHPublicKey).Should(Equal(publicKey))

		h, err := bmclient.Inventory.GetHost(ctx, &inventory.GetHostParams{HostID: *host1.GetPayload().ID})
		Expect(err).NotTo(HaveOccurred())
		Expect(h.GetPayload().Role).Should(Equal("master"))

		h, err = bmclient.Inventory.GetHost(ctx, &inventory.GetHostParams{HostID: *host2.GetPayload().ID})
		Expect(err).NotTo(HaveOccurred())
		Expect(h.GetPayload().Role).Should(Equal("worker"))
	})
})
