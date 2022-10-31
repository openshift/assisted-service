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

var _ = Describe("multipleVips", func() {
	var (
		db        *gorm.DB
		dbName    string
		clusterID strfmt.UUID
		cluster   *common.Cluster
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID,
			},
		}
		Expect(db.Save(cluster).Error).ShouldNot(HaveOccurred())

		_, err := common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
		Expect(err).ShouldNot(HaveOccurred())

		var value []string
		db.Raw("UPDATE clusters SET api_vip = ? WHERE id = ? RETURNING id", "192.168.111.10", clusterID).Scan(&value)
		db.Raw("UPDATE clusters SET ingress_vip = ? WHERE id = ? RETURNING id", "192.168.111.11", clusterID).Scan(&value)

		db.Raw("SELECT api_vip FROM clusters WHERE id = ?", clusterID).Scan(&value)
		Expect(value[0]).To(Equal("192.168.111.10"))
		db.Raw("SELECT ingress_vip FROM clusters WHERE id = ?", clusterID).Scan(&value)
		Expect(value[0]).To(Equal("192.168.111.11"))
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates up", func() {
		// setup
		err := migrateTo(db, "20221031103047")
		Expect(err).NotTo(HaveOccurred())

		// test
		var valueStr []string
		var valueNum []int
		db.Raw("SELECT COUNT(ip) FROM api_vips WHERE cluster_id = ?", clusterID).Scan(&valueNum)
		Expect(valueNum[0]).To(Equal(1))
		db.Raw("SELECT ip FROM api_vips WHERE cluster_id = ?", clusterID).Scan(&valueStr)
		Expect(valueStr[0]).To(Equal("192.168.111.10"))
		db.Raw("SELECT COUNT(ip) FROM ingress_vips WHERE cluster_id = ?", clusterID).Scan(&valueNum)
		Expect(valueNum[0]).To(Equal(1))
		db.Raw("SELECT ip FROM ingress_vips WHERE cluster_id = ?", clusterID).Scan(&valueStr)
		Expect(valueStr[0]).To(Equal("192.168.111.11"))
	})

	It("Migrates down", func() {
		err := migrateTo(db, "20221031103047")
		Expect(err).NotTo(HaveOccurred())

		// setup
		err = gormigrate.New(db, gormigrate.DefaultOptions, post()).RollbackMigration(multipleVips())
		Expect(err).NotTo(HaveOccurred())

		// test
		var valueNum []int
		db.Raw("SELECT COUNT(ip) FROM api_vips WHERE cluster_id = ?", clusterID).Scan(&valueNum)
		Expect(valueNum[0]).To(Equal(0))
		db.Raw("SELECT COUNT(ip) FROM ingress_vips WHERE cluster_id = ?", clusterID).Scan(&valueNum)
		Expect(valueNum[0]).To(Equal(0))
	})
})
