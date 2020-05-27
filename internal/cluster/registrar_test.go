package cluster

import (
	context "context"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("registrar", func() {
	var (
		ctx             = context.Background()
		registerManager RegistrationAPI
		db              *gorm.DB
		id              strfmt.UUID
		updateErr       error
		cluster         common.Cluster
		host            models.Host
	)

	BeforeEach(func() {
		db = prepareDB()
		registerManager = NewRegistrar(getTestLog(), db)

		id = strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(clusterStatusInsufficient),
		}}

		//register cluster
		updateErr = registerManager.RegisterCluster(ctx, &cluster)
		Expect(updateErr).Should(BeNil())
		Expect(swag.StringValue(cluster.Status)).Should(Equal(clusterStatusInsufficient))
		cluster = geCluster(*cluster.ID, db)
		Expect(swag.StringValue(cluster.Status)).Should(Equal(clusterStatusInsufficient))
	})

	Context("register cluster", func() {
		It("register a registered cluster", func() {
			updateErr = registerManager.RegisterCluster(ctx, &cluster)
			Expect(updateErr).Should(HaveOccurred())

			cluster = geCluster(*cluster.ID, db)
			Expect(swag.StringValue(cluster.Status)).Should(Equal(clusterStatusInsufficient))
		})
	})

	Context("deregister", func() {
		It("unregister a registered cluster", func() {
			updateErr = registerManager.DeregisterCluster(ctx, &cluster)
			Expect(updateErr).Should(BeNil())

			Expect(db.Preload("Hosts").First(&cluster, "id = ?", cluster.ID).Error).Should(HaveOccurred())

			Expect(db.First(&cluster, "id = ?", cluster.ID).Error).Should(HaveOccurred())
			Expect(db.First(&host, "cluster_id = ?", cluster.ID).Error).Should(HaveOccurred())

		})
		It("unregister a cluster in installing state", func() {
			// cluster state to installing
			cluster.Status = swag.String("installing")
			Expect(db.Model(cluster).Update("Status", "installing").Error).NotTo(HaveOccurred())

			updateErr = registerManager.DeregisterCluster(ctx, &cluster)
			Expect(updateErr).Should(HaveOccurred())

			db.First(&cluster, "id = ?", cluster.ID)
			Expect(db.First(&cluster, "id = ?", cluster.ID).Error).NotTo(HaveOccurred())
			Expect(swag.StringValue(cluster.Status)).Should(Equal(clusterStatusInstalling))

		})
	})

	AfterEach(func() {
		db.Close()
		updateErr = nil
	})
})
