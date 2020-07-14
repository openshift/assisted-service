package host

import (
	"fmt"

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
	)

	BeforeEach(func() {
		db = prepareDB()
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, defaultStatus)
		host.StatusInfo = &defaultStatusInfo
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		lastUpdatedTime = host.StatusUpdatedAt
	})

	Describe("updateHostStateWithParams", func() {
		It("change_status", func() {
			Expect(updateHostStateWithParams(getTestLog(), newStatus, newStatusInfo, &host, db)).ShouldNot(HaveOccurred())
			Expect(*host.Status).Should(Equal(newStatus))
			Expect(*host.StatusInfo).Should(Equal(newStatusInfo))
			Expect(host.StatusUpdatedAt).ShouldNot(Equal(lastUpdatedTime))
		})

		Describe("negative", func() {
			It("invalid_extras_amount", func() {
				Expect(updateHostStateWithParams(getTestLog(), newStatus, newStatusInfo, &host, db, "1")).Should(HaveOccurred())
				Expect(updateHostStateWithParams(getTestLog(), newStatus, newStatusInfo, &host, db, "1", "2", "3")).Should(HaveOccurred())
			})

			It("no_matching_rows", func() {
				var otherStatus = "otherStatus"
				host.Status = &otherStatus
				Expect(updateHostStateWithParams(getTestLog(), newStatus, newStatusInfo, &host, db)).Should(HaveOccurred())
			})

			It("db_failure", func() {
				db.Close()
				Expect(updateHostStateWithParams(getTestLog(), newStatus, newStatusInfo, &host, db)).Should(HaveOccurred())
			})

			AfterEach(func() {
				Expect(*host.Status).ShouldNot(Equal(newStatus))
				Expect(*host.StatusInfo).ShouldNot(Equal(newStatusInfo))
				Expect(host.StatusUpdatedAt).Should(Equal(lastUpdatedTime))
			})
		})
	})

	Describe("updateHostProgress", func() {
		Describe("same_status", func() {
			It("new_stage", func() {
				Expect(updateHostProgress(getTestLog(), defaultStatus, defaultStatusInfo, &host, db,
					defaultProgressStage, host.Progress.ProgressInfo)).ShouldNot(HaveOccurred())

				Expect(host.Progress.CurrentStage).Should(Equal(defaultProgressStage))
				Expect(host.Progress.ProgressInfo).Should(Equal(host.Progress.ProgressInfo))
				Expect(host.StageUpdatedAt).ShouldNot(Equal(lastUpdatedTime))
				Expect(host.StageStartedAt).ShouldNot(Equal(lastUpdatedTime))
			})

			It("same_stage", func() {
				// Still updates because stage_updated_at is being updated
				Expect(updateHostProgress(getTestLog(), defaultStatus, defaultStatusInfo, &host, db,
					host.Progress.CurrentStage, host.Progress.ProgressInfo)).ShouldNot(HaveOccurred())

				Expect(host.Progress.CurrentStage).Should(Equal(models.HostStage("")))
				Expect(host.Progress.ProgressInfo).Should(Equal(""))
				Expect(host.StageUpdatedAt).ShouldNot(Equal(lastUpdatedTime))
				Expect(host.StageStartedAt).Should(Equal(lastUpdatedTime))
			})

			AfterEach(func() {
				By("Same status info", func() {
					Expect(*host.Status).Should(Equal(defaultStatus))
					Expect(*host.StatusInfo).Should(Equal(defaultStatusInfo))
					Expect(host.StatusUpdatedAt).Should(Equal(lastUpdatedTime))
				})
			})
		})

		It("new_status_new_stage", func() {
			Expect(updateHostProgress(getTestLog(), newStatus, newStatusInfo, &host, db,
				defaultProgressStage, "")).ShouldNot(HaveOccurred())

			Expect(host.Progress.CurrentStage).Should(Equal(defaultProgressStage))
			Expect(host.Progress.ProgressInfo).Should(Equal(""))
			Expect(host.StageUpdatedAt).ShouldNot(Equal(lastUpdatedTime))
			Expect(host.StageStartedAt).ShouldNot(Equal(lastUpdatedTime))

			By("New status ", func() {
				Expect(*host.Status).Should(Equal(newStatus))
				Expect(*host.StatusInfo).Should(Equal(newStatusInfo))
				Expect(host.StatusUpdatedAt).ShouldNot(Equal(lastUpdatedTime))
			})
		})

		It("update_info", func() {
			for _, i := range []int{5, 10, 15} {
				Expect(updateHostProgress(getTestLog(), defaultStatus, defaultStatusInfo, &host, db,
					host.Progress.CurrentStage, fmt.Sprintf("%d%%", i))).ShouldNot(HaveOccurred())
				Expect(host.Progress.ProgressInfo).Should(Equal(fmt.Sprintf("%d%%", i)))
				Expect(host.StageStartedAt).Should(Equal(lastUpdatedTime))
			}
		})
	})
})
