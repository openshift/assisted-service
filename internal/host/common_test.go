package host

import (
	"fmt"

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

var _ = Describe("update_host_state", func() {
	var (
		db              *gorm.DB
		host            models.Host
		lastUpdatedTime strfmt.DateTime
		returnedHost    *models.Host
		err             error
		dbName          string = "host_common_test"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, defaultStatus)
		host.StatusInfo = &defaultStatusInfo
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		lastUpdatedTime = host.StatusUpdatedAt
	})

	Describe("updateHostStatus", func() {
		It("change_status", func() {
			returnedHost, err = updateHostStatus(getTestLog(), db, host.ClusterID, *host.ID, defaultStatus,
				newStatus, newStatusInfo)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*returnedHost.Status).Should(Equal(newStatus))
			Expect(*returnedHost.StatusInfo).Should(Equal(newStatusInfo))
			Expect(returnedHost.StatusUpdatedAt.String()).ShouldNot(Equal(lastUpdatedTime.String()))
		})

		Describe("negative", func() {
			It("invalid_extras_amount", func() {
				returnedHost, err = updateHostStatus(getTestLog(), db, host.ClusterID, *host.ID, *host.Status,
					newStatus, newStatusInfo, "1")
				Expect(err).Should(HaveOccurred())
				Expect(returnedHost).Should(BeNil())
				returnedHost, err = updateHostStatus(getTestLog(), db, host.ClusterID, *host.ID, *host.Status,
					newStatus, newStatusInfo, "1", "2", "3")
			})

			It("no_matching_rows", func() {
				returnedHost, err = updateHostStatus(getTestLog(), db, host.ClusterID, *host.ID, "otherStatus",
					newStatus, newStatusInfo)
			})

			AfterEach(func() {
				Expect(err).Should(HaveOccurred())
				Expect(returnedHost).Should(BeNil())

				hostFromDb := getHost(*host.ID, host.ClusterID, db)
				Expect(*hostFromDb.Status).ShouldNot(Equal(newStatus))
				Expect(*hostFromDb.StatusInfo).ShouldNot(Equal(newStatusInfo))
				Expect(hostFromDb.StatusUpdatedAt.String()).Should(Equal(lastUpdatedTime.String()))
			})
		})

		It("db_failure", func() {
			db.Close()
			_, err = updateHostStatus(getTestLog(), db, host.ClusterID, *host.ID, *host.Status,
				newStatus, newStatusInfo)
			Expect(err).Should(HaveOccurred())
		})
	})

	Describe("updateHostProgress", func() {
		Describe("same_status", func() {
			It("new_stage", func() {
				returnedHost, err = updateHostProgress(getTestLog(), db, host.ClusterID, *host.ID, *host.Status, defaultStatus, defaultStatusInfo,
					host.Progress.CurrentStage, defaultProgressStage, host.Progress.ProgressInfo)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(returnedHost.Progress.CurrentStage).Should(Equal(defaultProgressStage))
				Expect(returnedHost.Progress.ProgressInfo).Should(Equal(host.Progress.ProgressInfo))
				Expect(returnedHost.Progress.StageUpdatedAt.String()).ShouldNot(Equal(lastUpdatedTime.String()))
				Expect(returnedHost.Progress.StageStartedAt.String()).ShouldNot(Equal(lastUpdatedTime.String()))
			})

			It("same_stage", func() {
				// Still updates because stage_updated_at is being updated
				returnedHost, err = updateHostProgress(getTestLog(), db, host.ClusterID, *host.ID, *host.Status, defaultStatus, defaultStatusInfo,
					host.Progress.CurrentStage, host.Progress.CurrentStage, host.Progress.ProgressInfo)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(returnedHost.Progress.CurrentStage).Should(Equal(models.HostStage("")))
				Expect(returnedHost.Progress.ProgressInfo).Should(Equal(""))
				Expect(returnedHost.Progress.StageUpdatedAt.String()).ShouldNot(Equal(lastUpdatedTime.String()))
				Expect(returnedHost.Progress.StageStartedAt.String()).Should(Equal(lastUpdatedTime.String()))
			})

			AfterEach(func() {
				By("Same status info", func() {
					Expect(*returnedHost.Status).Should(Equal(defaultStatus))
					Expect(*returnedHost.StatusInfo).Should(Equal(defaultStatusInfo))
					Expect(returnedHost.StatusUpdatedAt.String()).Should(Equal(lastUpdatedTime.String()))
				})
			})
		})

		It("new_status_new_stage", func() {
			returnedHost, err = updateHostProgress(getTestLog(), db, host.ClusterID, *host.ID, *host.Status, newStatus, newStatusInfo,
				host.Progress.CurrentStage, defaultProgressStage, "")
			Expect(err).ShouldNot(HaveOccurred())

			Expect(returnedHost.Progress.CurrentStage).Should(Equal(defaultProgressStage))
			Expect(returnedHost.Progress.ProgressInfo).Should(Equal(""))
			Expect(returnedHost.Progress.StageUpdatedAt.String()).ShouldNot(Equal(lastUpdatedTime.String()))
			Expect(returnedHost.Progress.StageStartedAt.String()).ShouldNot(Equal(lastUpdatedTime.String()))

			By("New status", func() {
				Expect(*returnedHost.Status).Should(Equal(newStatus))
				Expect(*returnedHost.StatusInfo).Should(Equal(newStatusInfo))
				Expect(returnedHost.StatusUpdatedAt.String()).ShouldNot(Equal(lastUpdatedTime.String()))
			})
		})

		It("update_info", func() {
			for _, i := range []int{5, 10, 15} {
				returnedHost, err = updateHostProgress(getTestLog(), db, host.ClusterID, *host.ID, *host.Status, defaultStatus, defaultStatusInfo,
					host.Progress.CurrentStage, host.Progress.CurrentStage, fmt.Sprintf("%d%%", i))
				Expect(err).ShouldNot(HaveOccurred())
				Expect(returnedHost.Progress.ProgressInfo).Should(Equal(fmt.Sprintf("%d%%", i)))
				Expect(returnedHost.Progress.StageStartedAt.String()).Should(Equal(lastUpdatedTime.String()))
			}
		})
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

})
