package cluster

import (
	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
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
		cluster         common.Cluster
		lastUpdatedTime strfmt.DateTime
	)

	BeforeEach(func() {
		db = prepareDB()

		id := strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:         &id,
			Status:     &defaultStatus,
			StatusInfo: &defaultStatusInfo,
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		lastUpdatedTime = cluster.StatusUpdatedAt
	})

	Describe("updateClusterStateWithParams", func() {
		It("change_status", func() {
			Expect(updateClusterStateWithParams(getTestLog(), newStatus, newStatusInfo, &cluster, db)).ShouldNot(HaveOccurred())
			Expect(*cluster.Status).Should(Equal(newStatus))
			Expect(*cluster.StatusInfo).Should(Equal(newStatusInfo))
			Expect(cluster.StatusUpdatedAt).ShouldNot(Equal(lastUpdatedTime))
		})

		Describe("negative", func() {
			It("invalid_extras_amount", func() {
				Expect(updateClusterStateWithParams(getTestLog(), newStatus, newStatusInfo, &cluster, db, "1")).Should(HaveOccurred())
				Expect(updateClusterStateWithParams(getTestLog(), newStatus, newStatusInfo, &cluster, db, "1", "2", "3")).Should(HaveOccurred())
			})

			It("no_matching_rows", func() {
				var otherStatus = "otherStatus"
				cluster.Status = &otherStatus
				Expect(updateClusterStateWithParams(getTestLog(), newStatus, newStatusInfo, &cluster, db)).Should(HaveOccurred())
			})

			It("db_failure", func() {
				db.Close()
				Expect(updateClusterStateWithParams(getTestLog(), newStatus, newStatusInfo, &cluster, db)).Should(HaveOccurred())
			})

			AfterEach(func() {
				Expect(*cluster.Status).ShouldNot(Equal(newStatus))
				Expect(*cluster.StatusInfo).ShouldNot(Equal(newStatusInfo))
				Expect(cluster.StatusUpdatedAt).Should(Equal(lastUpdatedTime))
			})
		})
	})
})
