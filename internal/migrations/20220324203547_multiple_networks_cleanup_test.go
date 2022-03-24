package migrations

import (
	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("multipleNetworksCleanup", func() {
	var (
		db            *gorm.DB
		dbName        string
		clusterID     strfmt.UUID
		cluster       *common.Cluster
		clusterFromDb *common.Cluster
		clusters      []*models.Cluster
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				ID:              &clusterID,
				ClusterNetworks: common.TestDualStackNetworking.ClusterNetworks,
				ServiceNetworks: common.TestDualStackNetworking.ServiceNetworks,
				MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
			},
		}
		Expect(db.Save(cluster).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates up", func() {
		// setup

		err := migrateTo(db, "20220324203547")
		Expect(err).NotTo(HaveOccurred())

		// test

		clusterFromDb, err = common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(clusterFromDb.ClusterNetworks).To(HaveLen(2))
		Expect(clusterFromDb.ServiceNetworks).To(HaveLen(2))
		Expect(clusterFromDb.MachineNetworks).To(HaveLen(2))

		var res bool

		res = db.Migrator().HasColumn(&clusters, "cluster_network_cidr")
		Expect(res).To(Equal(false))
		res = db.Migrator().HasColumn(&clusters, "cluster_network_host_prefix")
		Expect(res).To(Equal(false))
		res = db.Migrator().HasColumn(&clusters, "machine_network_cidr")
		Expect(res).To(Equal(false))
		res = db.Migrator().HasColumn(&clusters, "service_network_cidr")
		Expect(res).To(Equal(false))
	})

	It("Migrates down", func() {
		err := migrateTo(db, "20220324203547")
		Expect(err).NotTo(HaveOccurred())

		// setup

		err = gormigrate.New(db, gormigrate.DefaultOptions, post()).RollbackMigration(multipleNetworksCleanup())
		Expect(err).NotTo(HaveOccurred())

		// test
	})
})
