package migrations

import (
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"

	gormigrate "github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("multipleVips", func() {
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
				ID:         &clusterID,
				APIVip:     "192.168.111.10",
				IngressVip: "192.168.111.11",
			},
		}
		Expect(db.Save(cluster).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates up", func() {
		// setup

		err := migrateTo(db, "20221031103047")
		Expect(err).NotTo(HaveOccurred())

		// test
		clusterFromDb, err = common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(clusterFromDb.APIVips).To(HaveLen(1))
		Expect(string(clusterFromDb.APIVips[0].IP)).To(Equal("192.168.111.10"))
		Expect(clusterFromDb.APIVips[0].ClusterID).To(Equal(*cluster.ID))
		Expect(clusterFromDb.IngressVips).To(HaveLen(1))
		Expect(string(clusterFromDb.IngressVips[0].IP)).To(Equal("192.168.111.11"))
		Expect(clusterFromDb.IngressVips[0].ClusterID).To(Equal(*cluster.ID))
	})

	It("Migrates down", func() {
		err := migrateTo(db, "20221031103047")
		Expect(err).NotTo(HaveOccurred())

		// setup

		err = gormigrate.New(db, gormigrate.DefaultOptions, post()).RollbackMigration(multipleVips())
		Expect(err).NotTo(HaveOccurred())

		// test
		clusterFromDb, err = common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(clusterFromDb.APIVips).To(HaveLen(0))
		Expect(clusterFromDb.IngressVips).To(HaveLen(0))
	})
})
