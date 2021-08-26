package cluster

import (
	context "context"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("registrar", func() {
	var (
		ctx             = context.Background()
		registerManager RegistrationAPI
		db              *gorm.DB
		id              strfmt.UUID
		updateErr       error
		cluster         common.Cluster
		infraEnv        *common.InfraEnv
		host            models.Host
		dbName          string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		registerManager = NewRegistrar(common.GetTestLog(), db)

		id = strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(models.ClusterStatusInsufficient),
		}}

		//register cluster
		updateErr = registerManager.RegisterCluster(ctx, &cluster, true)
		Expect(updateErr).Should(BeNil())
		Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusInsufficient))
		cluster = getClusterFromDB(*cluster.ID, db)
		Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusInsufficient))
		infraEnv, updateErr = common.GetInfraEnvFromDB(db, id)
		Expect(updateErr).Should(BeNil())
	})

	Context("register cluster", func() {
		It("register a registered cluster", func() {
			updateErr = registerManager.RegisterCluster(ctx, &cluster, true)
			Expect(updateErr).Should(HaveOccurred())

			cluster = getClusterFromDB(*cluster.ID, db)
			Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusInsufficient))
		})

		It("register a (soft) deleted cluster", func() {
			Expect(db.Unscoped().Delete(&cluster).Error).ShouldNot(HaveOccurred())
			Expect(db.Unscoped().Delete(infraEnv).Error).ShouldNot(HaveOccurred())

			updateErr = registerManager.RegisterCluster(ctx, &cluster, true)
			Expect(updateErr).ShouldNot(HaveOccurred())

			cluster = getClusterFromDB(*cluster.ID, db)
			Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusInsufficient))

			updateErr = registerManager.DeregisterCluster(ctx, &cluster)
			Expect(updateErr).ShouldNot(HaveOccurred())
			Expect(db.First(&common.Cluster{}, "id = ?", cluster.ID).RowsAffected).Should(Equal(int64(0)))
			Expect(db.Unscoped().First(&common.Cluster{}, "id = ?", cluster.ID).RowsAffected).Should(Equal(int64(1)))

			updateErr = registerManager.RegisterCluster(ctx, &cluster, true)
			Expect(updateErr).ShouldNot(HaveOccurred())

			cluster = getClusterFromDB(*cluster.ID, db)
			Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusInsufficient))
		})
	})

	Context("deregister", func() {
		It("unregister a registered cluster", func() {
			updateErr = registerManager.DeregisterCluster(ctx, &cluster)
			Expect(updateErr).Should(BeNil())

			_, err := common.GetClusterFromDB(db, *cluster.ID, common.UseEagerLoading)
			Expect(err).Should(HaveOccurred())

			Expect(db.First(&common.Cluster{}, "id = ?", cluster.ID).Error).Should(HaveOccurred())
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
			Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusInstalling))

		})
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		updateErr = nil
	})
})
