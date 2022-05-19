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

var _ = Describe("multipleNetworks", func() {
	var (
		db            *gorm.DB
		dbName        string
		clusterID     strfmt.UUID
		cluster       *common.Cluster
		clusterFromDb *common.Cluster
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()

		clusterID = strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				ID:                       &clusterID,
				ClusterNetworkCidr:       string(common.TestIPv4Networking.ClusterNetworks[0].Cidr),
				ClusterNetworkHostPrefix: common.TestIPv4Networking.ClusterNetworks[0].HostPrefix,
				ServiceNetworkCidr:       string(common.TestIPv4Networking.ServiceNetworks[0].Cidr),
				MachineNetworkCidr:       string(common.TestIPv4Networking.MachineNetworks[0].Cidr),
			},
		}
		Expect(db.Save(cluster).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates up", func() {
		// setup

		err := migrateTo(db, "20210822134659")
		Expect(err).NotTo(HaveOccurred())

		// test
		clusterFromDb, err = common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(clusterFromDb.ClusterNetworks).To(HaveLen(1))
		Expect(string(clusterFromDb.ClusterNetworks[0].Cidr)).To(Equal(cluster.ClusterNetworkCidr))
		Expect(clusterFromDb.ClusterNetworks[0].HostPrefix).To(Equal(cluster.ClusterNetworkHostPrefix))
		Expect(clusterFromDb.ClusterNetworks[0].ClusterID).To(Equal(*cluster.ID))
		Expect(clusterFromDb.ServiceNetworks).To(HaveLen(1))
		Expect(string(clusterFromDb.ServiceNetworks[0].Cidr)).To(Equal(cluster.ServiceNetworkCidr))
		Expect(clusterFromDb.ServiceNetworks[0].ClusterID).To(Equal(*cluster.ID))
		Expect(clusterFromDb.MachineNetworks).To(HaveLen(1))
		Expect(string(clusterFromDb.MachineNetworks[0].Cidr)).To(Equal(cluster.MachineNetworkCidr))
		Expect(clusterFromDb.MachineNetworks[0].ClusterID).To(Equal(*cluster.ID))
	})

	It("Migrates down", func() {
		err := migrateTo(db, "20210822134659")
		Expect(err).NotTo(HaveOccurred())

		// setup

		err = gormigrate.New(db, gormigrate.DefaultOptions, post()).RollbackMigration(multipleNetworks())
		Expect(err).NotTo(HaveOccurred())

		// test
		clusterFromDb, err = common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(clusterFromDb.ClusterNetworks).To(HaveLen(0))
		Expect(clusterFromDb.ServiceNetworks).To(HaveLen(0))
		Expect(clusterFromDb.MachineNetworks).To(HaveLen(0))
	})
})
