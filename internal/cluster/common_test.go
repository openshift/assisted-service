package cluster

import (
	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var defaultStatus = "status"
var defaultStatusInfo = "statusInfo"
var newStatus = "newStatus"
var newStatusInfo = "newStatusInfo"

var _ = Describe("update_cluster_state", func() {
	var (
		db              *gorm.DB
		cluster         *common.Cluster
		lastUpdatedTime strfmt.DateTime
		err             error
		dbName          string = "common_test"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)

		id := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:         &id,
			Status:     &defaultStatus,
			StatusInfo: &defaultStatusInfo,
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		lastUpdatedTime = cluster.StatusUpdatedAt
	})

	Describe("UpdateCluster", func() {
		It("change_status", func() {
			cluster, err = UpdateCluster(getTestLog(), db, *cluster.ID, *cluster.Status, "status", newStatus, "status_info", newStatusInfo)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(cluster.Status)).Should(Equal(newStatus))
			Expect(*cluster.StatusInfo).Should(Equal(newStatusInfo))
		})

		Describe("negative", func() {
			It("invalid_extras_amount", func() {
				_, err = UpdateCluster(getTestLog(), db, *cluster.ID, *cluster.Status, "1")
				Expect(err).Should(HaveOccurred())
				_, err = UpdateCluster(getTestLog(), db, *cluster.ID, *cluster.Status, "1", "2", "3")
				Expect(err).Should(HaveOccurred())
			})

			It("no_matching_rows", func() {
				_, err = UpdateCluster(getTestLog(), db, *cluster.ID, "otherStatus", "status", newStatus)
				Expect(err).Should(HaveOccurred())
			})

			AfterEach(func() {
				Expect(db.First(&cluster, "id = ?", cluster.ID).Error).ShouldNot(HaveOccurred())
				Expect(*cluster.Status).ShouldNot(Equal(newStatus))
				Expect(*cluster.StatusInfo).ShouldNot(Equal(newStatusInfo))
				Expect(cluster.StatusUpdatedAt).Should(Equal(lastUpdatedTime))
			})
		})

		It("db_failure", func() {
			db.Close()
			_, err = UpdateCluster(getTestLog(), db, *cluster.ID, *cluster.Status, "status", newStatus)
			Expect(err).Should(HaveOccurred())
		})
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

})
