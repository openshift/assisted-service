package hostutil

import (
	"context"
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var newStatus = "newStatus"
var newStatusInfo = "newStatusInfo"

var _ = Describe("update_host_state", func() {
	var (
		ctx             = context.Background()
		ctrl            *gomock.Controller
		db              *gorm.DB
		mockEvents      *eventsapi.MockHandler
		host            models.Host
		lastUpdatedTime strfmt.DateTime
		returnedHost    *common.Host
		err             error
		dbName          string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		infraEnvId := strfmt.UUID(uuid.New().String())
		host = GenerateTestHost(id, infraEnvId, clusterId, common.TestDefaultConfig.Status)
		host.StatusInfo = &common.TestDefaultConfig.StatusInfo
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		lastUpdatedTime = host.StatusUpdatedAt
	})

	Describe("UpdateHostStatus", func() {
		It("change_status", func() {
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(host.ID.String()),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String())))
			returnedHost, err = UpdateHostStatus(ctx, common.GetTestLog(), db, mockEvents, host.InfraEnvID, *host.ID, common.TestDefaultConfig.Status,
				newStatus, newStatusInfo)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*returnedHost.Status).Should(Equal(newStatus))
			Expect(*returnedHost.StatusInfo).Should(Equal(newStatusInfo))
			Expect(returnedHost.StatusUpdatedAt.String()).ShouldNot(Equal(lastUpdatedTime.String()))
		})

		Describe("negative", func() {
			It("invalid_extras_amount", func() {
				returnedHost, err = UpdateHostStatus(ctx, common.GetTestLog(), db, mockEvents, host.InfraEnvID, *host.ID, *host.Status,
					newStatus, newStatusInfo, "1")
				Expect(err).Should(HaveOccurred())
				Expect(returnedHost).Should(BeNil())
				returnedHost, err = UpdateHostStatus(ctx, common.GetTestLog(), db, mockEvents, host.InfraEnvID, *host.ID, *host.Status,
					newStatus, newStatusInfo, "1", "2", "3")
			})

			It("no_matching_rows", func() {
				returnedHost, err = UpdateHostStatus(ctx, common.GetTestLog(), db, mockEvents, host.InfraEnvID, *host.ID, "otherStatus",
					newStatus, newStatusInfo)
			})

			AfterEach(func() {
				Expect(err).Should(HaveOccurred())
				Expect(returnedHost).Should(BeNil())

				hostFromDb := GetHostFromDB(*host.ID, host.InfraEnvID, db)
				Expect(*hostFromDb.Status).ShouldNot(Equal(newStatus))
				Expect(*hostFromDb.StatusInfo).ShouldNot(Equal(newStatusInfo))
				Expect(hostFromDb.StatusUpdatedAt.String()).Should(Equal(lastUpdatedTime.String()))
			})
		})

		It("db_failure", func() {
			common.CloseDB(db)
			_, err = UpdateHostStatus(ctx, common.GetTestLog(), db, mockEvents, host.InfraEnvID, *host.ID, *host.Status,
				newStatus, newStatusInfo)
			Expect(err).Should(HaveOccurred())
		})
	})

	Describe("UpdateHostProgress", func() {
		Describe("same_status", func() {
			It("new_stage", func() {
				returnedHost, err = UpdateHostProgress(ctx, common.GetTestLog(), db, mockEvents, host.InfraEnvID, *host.ID, *host.Status, common.TestDefaultConfig.Status, common.TestDefaultConfig.StatusInfo,
					host.Progress.CurrentStage, common.TestDefaultConfig.HostProgressStage, host.Progress.ProgressInfo)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(returnedHost.Progress.CurrentStage).Should(Equal(common.TestDefaultConfig.HostProgressStage))
				Expect(returnedHost.Progress.ProgressInfo).Should(Equal(host.Progress.ProgressInfo))
				Expect(returnedHost.Progress.StageUpdatedAt.String()).ShouldNot(Equal(lastUpdatedTime.String()))
				Expect(returnedHost.Progress.StageStartedAt.String()).ShouldNot(Equal(lastUpdatedTime.String()))
			})

			It("same_stage", func() {
				// Still updates because stage_updated_at is being updated
				returnedHost, err = UpdateHostProgress(ctx, common.GetTestLog(), db, mockEvents, host.InfraEnvID, *host.ID, *host.Status, common.TestDefaultConfig.Status, common.TestDefaultConfig.StatusInfo,
					host.Progress.CurrentStage, host.Progress.CurrentStage, host.Progress.ProgressInfo)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(returnedHost.Progress.CurrentStage).Should(Equal(models.HostStage("")))
				Expect(returnedHost.Progress.ProgressInfo).Should(Equal(""))
				Expect(returnedHost.Progress.StageUpdatedAt.String()).ShouldNot(Equal(lastUpdatedTime.String()))
				Expect(returnedHost.Progress.StageStartedAt.String()).Should(Equal(lastUpdatedTime.String()))
			})

			AfterEach(func() {
				By("Same status info", func() {
					Expect(*returnedHost.Status).Should(Equal(common.TestDefaultConfig.Status))
					Expect(*returnedHost.StatusInfo).Should(Equal(common.TestDefaultConfig.StatusInfo))
					Expect(returnedHost.StatusUpdatedAt.String()).Should(Equal(lastUpdatedTime.String()))
				})
			})
		})

		It("new_status_new_stage", func() {
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(host.ID.String()),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String())))
			returnedHost, err = UpdateHostProgress(ctx, common.GetTestLog(), db, mockEvents, host.InfraEnvID, *host.ID, *host.Status, newStatus, newStatusInfo,
				host.Progress.CurrentStage, common.TestDefaultConfig.HostProgressStage, "")
			Expect(err).ShouldNot(HaveOccurred())

			Expect(returnedHost.Progress.CurrentStage).Should(Equal(common.TestDefaultConfig.HostProgressStage))
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
				returnedHost, err = UpdateHostProgress(ctx, common.GetTestLog(), db, mockEvents, host.InfraEnvID, *host.ID, *host.Status, common.TestDefaultConfig.Status, common.TestDefaultConfig.StatusInfo,
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
