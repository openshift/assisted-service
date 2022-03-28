package migrations

import (
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
		db.Raw("UPDATE clusters SET cluster_network_cidr = ? WHERE id = ? RETURNING id", "1.3.0.0/16", clusterID).Scan(&value)
		db.Raw("UPDATE clusters SET cluster_network_host_prefix = ? WHERE id = ? RETURNING id", 24, clusterID).Scan(&value)
		db.Raw("UPDATE clusters SET machine_network_cidr = ? WHERE id = ? RETURNING id", "1.2.3.0/24", clusterID).Scan(&value)
		db.Raw("UPDATE clusters SET service_network_cidr = ? WHERE id = ? RETURNING id", "1.2.5.0/24", clusterID).Scan(&value)

		db.Raw("SELECT cluster_network_cidr FROM clusters WHERE id = ?", clusterID).Scan(&value)
		Expect(value[0]).To(Equal("1.3.0.0/16"))
		db.Raw("SELECT cluster_network_host_prefix FROM clusters WHERE id = ?", clusterID).Scan(&value)
		Expect(value[0]).To(Equal("24"))
		db.Raw("SELECT machine_network_cidr FROM clusters WHERE id = ?", clusterID).Scan(&value)
		Expect(value[0]).To(Equal("1.2.3.0/24"))
		db.Raw("SELECT service_network_cidr FROM clusters WHERE id = ?", clusterID).Scan(&value)
		Expect(value[0]).To(Equal("1.2.5.0/24"))
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates up", func() {
		// setup
		err := migrateTo(db, "20220325125504")
		Expect(err).NotTo(HaveOccurred())

		// test
		var valueStr []string
		db.Raw("SELECT cluster_network_cidr FROM clusters WHERE id = ?", clusterID).Scan(&valueStr)
		Expect(valueStr[0]).To(Equal(""))
		db.Raw("SELECT machine_network_cidr FROM clusters WHERE id = ?", clusterID).Scan(&valueStr)
		Expect(valueStr[0]).To(Equal(""))
		db.Raw("SELECT service_network_cidr FROM clusters WHERE id = ?", clusterID).Scan(&valueStr)
		Expect(valueStr[0]).To(Equal(""))

		var valueNum []int
		db.Raw("SELECT cluster_network_host_prefix FROM clusters WHERE id = ?", clusterID).Scan(&valueNum)
		Expect(valueNum[0]).To(Equal(0))
	})
})
