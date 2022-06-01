package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/internal/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/openshift/assisted-service/pkg/leader"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/types"
)

var (
	defaultHwInfo                  = "default hw info" // invalid hw info used only for tests
	defaultDisabledHostValidations = DisabledHostValidations{}
	defaultConfig                  = &Config{
		ResetTimeout:            3 * time.Minute,
		EnableAutoReset:         true,
		EnableAutoAssign:        true,
		MonitorBatchSize:        100,
		DisabledHostvalidations: defaultDisabledHostValidations,
	}
	defaultNTPSources = []*models.NtpSource{common.TestNTPSourceSynced}
)

var _ = Describe("update_role", func() {
	var (
		ctx                       = context.Background()
		db                        *gorm.DB
		state                     API
		host                      models.Host
		id, clusterID, infraEnvID strfmt.UUID
		dbName                    string
	)

	BeforeEach(func() {
		dummy := &leader.DummyElector{}
		db, dbName = common.PrepareTestDB()
		state = NewManager(common.GetTestLog(), db, nil, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil, nil)
		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("update role by src state", func() {
		success := func(srcState string) {
			host = hostutil.GenerateTestHost(id, infraEnvID, clusterID, srcState)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, nil)).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(id, infraEnvID, db)
			Expect(h.Role).To(Equal(models.HostRoleMaster))
		}

		failure := func(srcState string) {
			host = hostutil.GenerateTestHost(id, infraEnvID, clusterID, srcState)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, nil)).To(HaveOccurred())
			h := hostutil.GetHostFromDB(id, infraEnvID, db)
			Expect(h.Role).To(Equal(models.HostRoleWorker))
		}

		tests := []struct {
			name     string
			srcState string
			testFunc func(srcState string)
		}{
			{
				name:     "discovering",
				srcState: models.HostStatusDiscovering,
				testFunc: success,
			},
			{
				name:     "known",
				srcState: models.HostStatusKnown,
				testFunc: success,
			},
			{
				name:     "disconnected",
				srcState: models.HostStatusDisconnected,
				testFunc: success,
			},
			{
				name:     "insufficient",
				srcState: models.HostStatusInsufficient,
				testFunc: success,
			},
			{
				name:     "error",
				srcState: models.HostStatusError,
				testFunc: failure,
			},
			{
				name:     "installing",
				srcState: models.HostStatusInstalling,
				testFunc: failure,
			},
			{
				name:     "installed",
				srcState: models.HostStatusInstalled,
				testFunc: failure,
			},
			{
				name:     "installing-in-progress",
				srcState: models.HostStatusInstallingInProgress,
				testFunc: failure,
			},
			{
				name:     models.HostStatusBinding,
				srcState: models.HostStatusBinding,
				testFunc: success,
			},
			{
				name:     models.HostStatusDiscoveringUnbound,
				srcState: models.HostStatusDiscoveringUnbound,
				testFunc: success,
			},
			{
				name:     models.HostStatusInsufficientUnbound,
				srcState: models.HostStatusInsufficientUnbound,
				testFunc: success,
			},
			{
				name:     models.HostStatusDisconnectedUnbound,
				srcState: models.HostStatusDisconnectedUnbound,
				testFunc: success,
			},
			{
				name:     models.HostStatusKnownUnbound,
				srcState: models.HostStatusKnownUnbound,
				testFunc: success,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				t.testFunc(t.srcState)
			})
		}
	})

	It("update role with transaction", func() {
		host = hostutil.GenerateTestHost(id, infraEnvID, clusterID, models.HostStatusKnown)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		By("rollback transaction", func() {
			tx := db.Begin()
			Expect(tx.Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, tx)).NotTo(HaveOccurred())
			Expect(tx.Rollback().Error).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(id, infraEnvID, db)
			Expect(h.Role).Should(Equal(models.HostRoleWorker))
		})
		By("commit transaction", func() {
			tx := db.Begin()
			Expect(tx.Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, tx)).NotTo(HaveOccurred())
			Expect(tx.Commit().Error).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(id, infraEnvID, db)
			Expect(h.Role).Should(Equal(models.HostRoleMaster))
		})
	})

	It("update role master to worker", func() {
		By("update role worker to master")
		host = hostutil.GenerateTestHost(id, infraEnvID, clusterID, models.HostStatusKnown)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, nil)).NotTo(HaveOccurred())
		h := hostutil.GetHostFromDB(id, infraEnvID, db)
		Expect(h.Role).To(Equal(models.HostRoleMaster))
		By("update role master to worker")
		Expect(state.UpdateRole(ctx, &h.Host, models.HostRoleWorker, nil)).NotTo(HaveOccurred())
		h = hostutil.GetHostFromDB(id, infraEnvID, db)
		Expect(h.Role).To(Equal(models.HostRoleWorker))
	})

	Context("update machine config pool", func() {
		tests := []struct {
			name                      string
			day2                      bool
			role                      models.HostRole
			previousRole              models.HostRole
			previousMachineConfigPool *string
			expectedMachineConfigPool string
		}{
			{
				name:                      "day1",
				day2:                      false,
				role:                      models.HostRoleMaster,
				previousRole:              "",
				expectedMachineConfigPool: "",
			},
			{
				name:                      "day2-new-worker",
				day2:                      true,
				role:                      models.HostRoleWorker,
				previousRole:              "",
				expectedMachineConfigPool: string(models.HostRoleWorker),
			},
			{
				name:                      "day2-new-master",
				day2:                      true,
				role:                      models.HostRoleMaster,
				previousRole:              "",
				expectedMachineConfigPool: string(models.HostRoleMaster),
			},
			{
				name:                      "day2-update-auto-assign",
				day2:                      true,
				role:                      models.HostRoleMaster,
				previousRole:              models.HostRoleAutoAssign,
				expectedMachineConfigPool: string(models.HostRoleMaster),
			},
			{
				name:                      "day2-update-worker",
				day2:                      true,
				role:                      models.HostRoleMaster,
				previousRole:              models.HostRoleWorker,
				expectedMachineConfigPool: string(models.HostRoleMaster),
			},
			{
				name:                      "day2-customize-pool",
				day2:                      true,
				role:                      models.HostRoleMaster,
				previousRole:              models.HostRoleWorker,
				previousMachineConfigPool: swag.String("different_pool"),
				expectedMachineConfigPool: "different_pool",
			},
		}

		for _, t := range tests {
			t := t
			It(t.name, func() {
				// Setup
				if t.day2 {
					host = hostutil.GenerateTestHostAddedToCluster(id, infraEnvID, clusterID, models.HostStatusKnown)
				} else {
					host = hostutil.GenerateTestHost(id, infraEnvID, clusterID, models.HostStatusKnown)
				}

				host.Role = t.previousRole

				if t.previousMachineConfigPool != nil {
					host.MachineConfigPoolName = swag.StringValue(t.previousMachineConfigPool)
				} else {
					host.MachineConfigPoolName = string(t.previousRole)
				}

				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

				// Test
				Expect(state.UpdateRole(ctx, &host, t.role, nil)).NotTo(HaveOccurred())
				h := hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
				Expect(h.Role).To(Equal(t.role))
				Expect(h.MachineConfigPoolName).Should(Equal(t.expectedMachineConfigPool))
			})
		}
	})
})

var _ = Describe("update_progress", func() {
	var (
		ctx        = context.Background()
		db         *gorm.DB
		state      API
		host       models.Host
		ctrl       *gomock.Controller
		mockEvents *eventsapi.MockHandler
		mockMetric *metrics.MockAPI
		dbName     string
	)

	setDefaultReportHostInstallationMetrics := func(mockMetricApi *metrics.MockAPI) {
		mockMetricApi.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		state = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), mockMetric, defaultConfig, dummy, nil, nil)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		infraEnvId := strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(id, infraEnvId, clusterId, "")
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	Context("installing host", func() {
		var (
			progress   models.HostProgress
			hostFromDB *common.Host
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			mockMetric = metrics.NewMockAPI(ctrl)
			setDefaultReportHostInstallationMetrics(mockMetric)
			host.Status = swag.String(models.HostStatusInstalling)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		})

		Context("positive stages", func() {
			It("some_progress", func() {
				progress.CurrentStage = common.TestDefaultConfig.HostProgressStage
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
					eventstest.WithHostIdMatcher(host.ID.String()),
					eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
					eventstest.WithClusterIdMatcher(host.ClusterID.String()),
					eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
			})

			It("same_value", func() {
				progress.CurrentStage = common.TestDefaultConfig.HostProgressStage
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
					eventstest.WithHostIdMatcher(host.ID.String()),
					eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
					eventstest.WithClusterIdMatcher(host.ClusterID.String()),
					eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
				updatedAt := hostFromDB.StageUpdatedAt.String()

				Expect(state.UpdateInstallProgress(ctx, &hostFromDB.Host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*hostFromDB.ID, host.InfraEnvID, db)
				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
				Expect(hostFromDB.StageUpdatedAt.String()).Should(Equal(updatedAt))
			})

			It("writing to disk", func() {
				progress.CurrentStage = models.HostStageWritingImageToDisk
				progress.ProgressInfo = "20%"
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
					eventstest.WithHostIdMatcher(host.ID.String()),
					eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
					eventstest.WithClusterIdMatcher(host.ClusterID.String()),
					eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
			})

			It("done", func() {
				progress.CurrentStage = models.HostStageDone
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
					eventstest.WithHostIdMatcher(host.ID.String()),
					eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
					eventstest.WithClusterIdMatcher(host.ClusterID.String()),
					eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstalled))
			})

			AfterEach(func() {
				Expect(*hostFromDB.StatusInfo).Should(Equal(string(progress.CurrentStage)))
				Expect(hostFromDB.Progress.CurrentStage).Should(Equal(progress.CurrentStage))
				Expect(hostFromDB.Progress.ProgressInfo).Should(Equal(progress.ProgressInfo))
			})
		})

		Context("Negative stages", func() {
			It("progress_failed", func() {
				progress.CurrentStage = models.HostStageFailed
				progress.ProgressInfo = "reason"
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
					eventstest.WithHostIdMatcher(host.ID.String()),
					eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
					eventstest.WithClusterIdMatcher(host.ClusterID.String()),
					eventstest.WithSeverityMatcher(models.EventSeverityError)))
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusError))
				Expect(*hostFromDB.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", progress.CurrentStage, progress.ProgressInfo)))
			})

			It("progress_failed_empty_reason", func() {
				progress.CurrentStage = models.HostStageFailed
				progress.ProgressInfo = ""
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
					eventstest.WithHostIdMatcher(host.ID.String()),
					eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
					eventstest.WithClusterIdMatcher(host.ClusterID.String()),
					eventstest.WithSeverityMatcher(models.EventSeverityError)))
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusError))
				Expect(*hostFromDB.StatusInfo).Should(Equal(string(progress.CurrentStage)))
			})

			It("progress_failed_after_a_stage", func() {
				By("Some stage", func() {
					progress.CurrentStage = models.HostStageWritingImageToDisk
					progress.ProgressInfo = "20%"
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(host.ID.String()),
						eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
						eventstest.WithClusterIdMatcher(host.ClusterID.String()),
						eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
					Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
					hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
					Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
					Expect(*hostFromDB.StatusInfo).Should(Equal(string(progress.CurrentStage)))

					Expect(hostFromDB.Progress.CurrentStage).Should(Equal(progress.CurrentStage))
					Expect(hostFromDB.Progress.ProgressInfo).Should(Equal(progress.ProgressInfo))
				})

				By("Failed", func() {
					newProgress := models.HostProgress{
						CurrentStage: models.HostStageFailed,
						ProgressInfo: "reason",
					}
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(host.ID.String()),
						eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
						eventstest.WithClusterIdMatcher(host.ClusterID.String()),
						eventstest.WithSeverityMatcher(models.EventSeverityError)))
					Expect(state.UpdateInstallProgress(ctx, &hostFromDB.Host, &newProgress)).ShouldNot(HaveOccurred())
					hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
					Expect(*hostFromDB.Status).Should(Equal(models.HostStatusError))
					Expect(*hostFromDB.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", newProgress.CurrentStage, newProgress.ProgressInfo)))

					Expect(hostFromDB.Progress.CurrentStage).Should(Equal(progress.CurrentStage))
					Expect(hostFromDB.Progress.ProgressInfo).Should(Equal(progress.ProgressInfo))
				})
			})
		})

		Context("Invalid progress", func() {
			It("lower_stage", func() {
				verifyDb := func() {
					hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
					Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
					Expect(*hostFromDB.StatusInfo).Should(Equal(string(progress.CurrentStage)))

					Expect(hostFromDB.Progress.CurrentStage).Should(Equal(progress.CurrentStage))
					Expect(hostFromDB.Progress.ProgressInfo).Should(Equal(progress.ProgressInfo))
				}

				By("Some stage", func() {
					progress.CurrentStage = models.HostStageWritingImageToDisk
					progress.ProgressInfo = "20%"
					mockMetric.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(host.ID.String()),
						eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
						eventstest.WithClusterIdMatcher(host.ClusterID.String()),
						eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
					Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
					verifyDb()
				})

				By("Lower stage", func() {
					newProgress := models.HostProgress{
						CurrentStage: models.HostStageInstalling,
					}
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(host.ID.String()),
						eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
						eventstest.WithClusterIdMatcher(host.ClusterID.String()),
						eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
					Expect(state.UpdateInstallProgress(ctx, &hostFromDB.Host, &newProgress)).Should(HaveOccurred())
					verifyDb()
				})
			})

			It("update_on_installed", func() {
				verifyDb := func() {
					hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)

					Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstalled))
					Expect(hostFromDB.StatusInfo).Should(BeNil())
					Expect(hostFromDB.Progress.CurrentStage).Should(BeEmpty())
					Expect(hostFromDB.Progress.ProgressInfo).Should(BeEmpty())
				}

				Expect(db.Model(&host).Updates(map[string]interface{}{"status": swag.String(models.HostStatusInstalled)}).Error).To(Not(HaveOccurred()))
				verifyDb()

				progress.CurrentStage = models.HostStageRebooting
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).Should(HaveOccurred())

				verifyDb()
			})
		})
	})

	It("invalid stage", func() {
		Expect(state.UpdateInstallProgress(ctx, &host,
			&models.HostProgress{CurrentStage: common.TestDefaultConfig.HostProgressStage})).Should(HaveOccurred())
	})
})

var _ = Describe("update progress special cases", func() {
	var (
		ctx        = context.Background()
		db         *gorm.DB
		state      API
		host       models.Host
		ctrl       *gomock.Controller
		mockEvents *eventsapi.MockHandler
		mockMetric *metrics.MockAPI
		dbName     string
		clusterId  strfmt.UUID
		infraEnvId strfmt.UUID
	)

	setDefaultReportHostInstallationMetrics := func(mockMetricApi *metrics.MockAPI) {
		mockMetricApi.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		setDefaultReportHostInstallationMetrics(mockMetric)
		state = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), mockMetric, defaultConfig, dummy, nil, nil)
		id := strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHostByKind(id, infraEnvId, &clusterId, models.HostStatusInstalling, models.HostKindHost, models.HostRoleMaster)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBootstrapSetEventName),
			eventstest.WithHostIdMatcher(host.ID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvId.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
		Expect(state.SetBootstrap(ctx, &host, true, db)).ShouldNot(HaveOccurred())

	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
	Context("installing host", func() {
		var (
			progress   models.HostProgress
			hostFromDB *common.Host
		)
		It("Single node special stage order - happy flow", func() {
			cluster := hostutil.GenerateTestCluster(clusterId, common.TestIPv4Networking.MachineNetworks)
			cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

			progress.CurrentStage = models.HostStageWaitingForBootkube
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(host.ID.String()),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
			Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
			hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
			Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
			Expect(hostFromDB.Progress.CurrentStage).Should(Equal(models.HostStageWaitingForBootkube))

			progress.CurrentStage = models.HostStageWritingImageToDisk
			progress.ProgressInfo = "20%"
			Expect(state.UpdateInstallProgress(ctx, &hostFromDB.Host, &progress)).ShouldNot(HaveOccurred())
			hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
			Expect(hostFromDB.Progress.CurrentStage).Should(Equal(models.HostStageWritingImageToDisk))
		})
		It("Single node special stage order - not allowed", func() {
			cluster := hostutil.GenerateTestCluster(clusterId, common.TestIPv4Networking.MachineNetworks)
			cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

			progress.CurrentStage = models.HostStageWaitingForBootkube
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(host.ID.String()),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
			Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
			hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
			Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
			Expect(hostFromDB.Progress.CurrentStage).Should(Equal(models.HostStageWaitingForBootkube))

			progress.CurrentStage = models.HostStageInstalling
			Expect(state.UpdateInstallProgress(ctx, &hostFromDB.Host, &progress)).Should(HaveOccurred())
		})
		It("multi node update should fail", func() {
			cluster := hostutil.GenerateTestCluster(clusterId, common.TestIPv4Networking.MachineNetworks)
			cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeFull)
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

			progress.CurrentStage = models.HostStageWaitingForBootkube
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(host.ID.String()),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
			Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
			hostFromDB = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
			Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
			Expect(hostFromDB.Progress.CurrentStage).Should(Equal(models.HostStageWaitingForBootkube))

			progress.CurrentStage = models.HostStageWritingImageToDisk
			progress.ProgressInfo = "20%"
			Expect(state.UpdateInstallProgress(ctx, &hostFromDB.Host, &progress)).Should(HaveOccurred())
		})
	})
})

var _ = Describe("cancel installation", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		h             models.Host
		eventsHandler eventsapi.Handler
		dbName        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		eventsHandler = events.New(db, nil, logrus.New())
		dummy := &leader.DummyElector{}
		state = NewManager(common.GetTestLog(), db, eventsHandler, nil, nil, nil, nil, defaultConfig, dummy, nil, nil)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		infraEnvId := strfmt.UUID(uuid.New().String())
		h = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusDiscovering)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("cancel installation", func() {
		It("cancel installation success", func() {
			c := common.Cluster{Cluster: models.Cluster{
				ID:                 h.ClusterID,
				Status:             swag.String(models.ClusterStatusError),
				OpenshiftClusterID: strfmt.UUID(uuid.New().String()),
			}}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			h.Status = swag.String(models.HostStatusInstalling)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
			events, err := eventsHandler.V2GetEvents(ctx, h.ClusterID, h.ID, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityInfo))
			eventMessage := fmt.Sprintf("Installation cancelled for host %s", hostutil.GetHostnameForMsg(&h))
			Expect(*cancelEvent.Message).Should(Equal(eventMessage))
		})

		It("cancel failed installation", func() {
			c := common.Cluster{Cluster: models.Cluster{
				ID:                 h.ClusterID,
				Status:             swag.String(models.ClusterStatusError),
				OpenshiftClusterID: strfmt.UUID(uuid.New().String()),
			}}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			h.Status = swag.String(models.HostStatusError)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
			events, err := eventsHandler.V2GetEvents(ctx, h.ClusterID, h.ID, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityInfo))
			eventMessage := fmt.Sprintf("Installation cancelled for host %s", hostutil.GetHostnameForMsg(&h))
			Expect(*cancelEvent.Message).Should(Equal(eventMessage))
		})

		AfterEach(func() {
			db.First(&h, "id = ? and cluster_id = ?", h.ID, *h.ClusterID)
			Expect(*h.Status).Should(Equal(models.HostStatusCancelled))
		})
	})

	Context("invalid cancel installation", func() {
		It("nothing to cancel", func() {
			c := common.Cluster{Cluster: models.Cluster{
				ID:                 h.ClusterID,
				OpenshiftClusterID: strfmt.UUID(uuid.New().String()),
			}}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).Should(HaveOccurred())
			events, err := eventsHandler.V2GetEvents(ctx, h.ClusterID, h.ID, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityError))
		})
	})
})

var _ = Describe("reset host", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		h             models.Host
		eventsHandler eventsapi.Handler
		dbName        string
		config        Config
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		eventsHandler = events.New(db, nil, logrus.New())
		config = *defaultConfig
		dummy := &leader.DummyElector{}
		state = NewManager(common.GetTestLog(), db, eventsHandler, nil, nil, nil, nil, &config, dummy, nil, nil)
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("reset installation", func() {
		It("reset installation success", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			infraEnvId := strfmt.UUID(uuid.New().String())
			c := common.Cluster{Cluster: models.Cluster{
				ID:                 &clusterId,
				Status:             swag.String(models.ClusterStatusError),
				OpenshiftClusterID: strfmt.UUID(uuid.New().String()),
			}}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			h = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusError)
			h.LogsCollectedAt = strfmt.DateTime(time.Now())
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(h.LogsCollectedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))
			Expect(state.ResetHost(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, *h.ClusterID)
			Expect(*h.Status).Should(Equal(models.HostStatusResetting))
			events, err := eventsHandler.V2GetEvents(ctx, h.ClusterID, h.ID, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			resetEvent := events[len(events)-1]
			Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityInfo))
			eventMessage := fmt.Sprintf("Installation reset for host %s", hostutil.GetHostnameForMsg(&h))
			Expect(*resetEvent.Message).Should(Equal(eventMessage))
			Expect(time.Time(h.LogsCollectedAt).Equal(time.Time{})).Should(BeTrue())
		})

		It("register resetting host", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			infraEnvId := strfmt.UUID(uuid.New().String())
			h = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusResetting)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.RegisterHost(ctx, &h, db)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, *h.ClusterID)
			Expect(*h.Status).Should(Equal(models.HostStatusDiscovering))
		})

		It("reset pending user action - passed timeout", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			infraEnvId := strfmt.UUID(uuid.New().String())
			c := common.Cluster{Cluster: models.Cluster{
				ID:                 &clusterId,
				Status:             swag.String(models.ClusterStatusError),
				OpenshiftClusterID: strfmt.UUID(uuid.New().String()),
			}}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			h = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusResetting)
			then := time.Now().Add(-config.ResetTimeout)
			h.StatusUpdatedAt = strfmt.DateTime(then)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.IsRequireUserActionReset(&h)).Should(Equal(true))
			Expect(state.ResetPendingUserAction(ctx, &h, db)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, *h.ClusterID)
			Expect(*h.Status).Should(Equal(models.HostStatusResettingPendingUserAction))
			events, err := eventsHandler.V2GetEvents(ctx, &clusterId, h.ID, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			resetEvent := events[len(events)-1]
			Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityInfo))
			eventMessage := fmt.Sprintf("User action is required in order to complete installation reset for host %s", hostutil.GetHostnameForMsg(&h))
			Expect(*resetEvent.Message).Should(Equal(eventMessage))
		})

		It("reset pending user action - host in reboot", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			infraEnvId := strfmt.UUID(uuid.New().String())
			c := common.Cluster{Cluster: models.Cluster{
				ID:                 &clusterId,
				Status:             swag.String(models.ClusterStatusError),
				OpenshiftClusterID: strfmt.UUID(uuid.New().String()),
			}}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			h = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusResetting)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			h.Progress.CurrentStage = models.HostStageRebooting
			Expect(state.IsRequireUserActionReset(&h)).Should(Equal(true))
			Expect(state.ResetPendingUserAction(ctx, &h, db)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, *h.ClusterID)
			Expect(*h.Status).Should(Equal(models.HostStatusResettingPendingUserAction))
			events, err := eventsHandler.V2GetEvents(ctx, h.ClusterID, h.ID, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			resetEvent := events[len(events)-1]
			Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityInfo))
			eventMessage := fmt.Sprintf("User action is required in order to complete installation reset for host %s", hostutil.GetHostnameForMsg(&h))
			Expect(*resetEvent.Message).Should(Equal(eventMessage))
		})
	})

	Context("invalid_reset_installation", func() {
		It("nothing_to_reset", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			infraEnvId := strfmt.UUID(uuid.New().String())
			c := common.Cluster{Cluster: models.Cluster{
				ID:                 &clusterId,
				OpenshiftClusterID: strfmt.UUID(uuid.New().String()),
			}}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			h = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusDiscovering)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			reply := state.ResetHost(ctx, &h, "some reason", db)
			Expect(int(reply.StatusCode())).Should(Equal(http.StatusConflict))
			events, err := eventsHandler.V2GetEvents(ctx, &clusterId, h.ID, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			resetEvent := events[len(events)-1]
			Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityError))
		})
	})

})

var _ = Describe("register host", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		ctrl          *gomock.Controller
		state         API
		h             models.Host
		eventsHandler *eventsapi.MockHandler
		dbName        string
		config        Config
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		eventsHandler = eventsapi.NewMockHandler(ctrl)
		config = *defaultConfig
		dummy := &leader.DummyElector{}
		state = NewManager(common.GetTestLog(), db, eventsHandler, nil, nil, nil, nil, &config, dummy, nil, nil)
	})

	BeforeEach(func() {
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		infraEnvId := strfmt.UUID(uuid.New().String())
		h = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusDiscovering)
	})

	It("register host success", func() {
		Expect(state.RegisterHost(ctx, &h, db)).ShouldNot(HaveOccurred())
		db.First(&h, "id = ? and cluster_id = ?", h.ID, *h.ClusterID)
		Expect(*h.Status).Should(Equal(models.HostStatusDiscovering))
	})

	It("register (soft) deleted host success", func() {
		Expect(state.RegisterHost(ctx, &h, db)).ShouldNot(HaveOccurred())
		db.First(&h, "id = ? and cluster_id = ?", h.ID, *h.ClusterID)
		Expect(*h.Status).Should(Equal(models.HostStatusDiscovering))
		Expect(db.Delete(&h).RowsAffected).Should(Equal(int64(1)))
		Expect(db.Unscoped().Find(&h).RowsAffected).Should(Equal(int64(1)))
		Expect(state.RegisterHost(ctx, &h, db)).ShouldNot(HaveOccurred())
		db.First(&h, "id = ? and cluster_id = ?", h.ID, *h.ClusterID)
		Expect(*h.Status).Should(Equal(models.HostStatusDiscovering))

	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

})

func insufficientHWInventory() string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 2},
		Disks: []*models.Disk{
			{
				SizeBytes: 130,
				DriveType: "HDD",
			},
		},
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
		Memory:       &models.Memory{PhysicalBytes: 130, UsableBytes: 130},
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func inventoryWithUnauthorizedVendor() string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 8},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: "HDD",
			},
		},
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
		Memory:       &models.Memory{PhysicalBytes: conversions.GibToBytes(16), UsableBytes: conversions.GibToBytes(16)},
		Hostname:     "master-hostname",
		SystemVendor: &models.SystemVendor{Manufacturer: "RDO", ProductName: "OpenStack Compute", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func workerInventory() string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 2},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: "HDD",
			},
		},
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
		Memory:       &models.Memory{PhysicalBytes: conversions.GibToBytes(8), UsableBytes: conversions.GibToBytes(8)},
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

var _ = Describe("UpdateInventory", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		ctrl                          *gomock.Controller
		mockEvents                    *eventsapi.MockHandler
		mockValidator                 *hardware.MockValidator
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		dbName                        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		ctrl = gomock.NewController(GinkgoT())
		mockValidator = hardware.NewMockValidator(ctrl)
		mockEvents = eventsapi.NewMockHandler(ctrl)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockValidator,
			nil, createValidatorCfg(), nil, defaultConfig, dummy, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		cluster := hostutil.GenerateTestCluster(clusterId, common.TestIPv4Networking.MachineNetworks)
		infraEnv := hostutil.GenerateTestInfraEnv(infraEnvId)
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	Context("Check populate disk id", func() {
		const (
			name   = "name"
			byPath = "/dev/disk/by-path/name"
			path   = "/dev/name"
		)
		for _, test := range []struct {
			testName   string
			inventory  models.Inventory
			expectedId string
		}{
			{testName: "Old agent - backwards compatibility - disk has by-path information",
				inventory: models.Inventory{Disks: []*models.Disk{
					{
						Name:   name,
						ByPath: byPath,
					},
				}},
				expectedId: path,
			}, {testName: "Old agent - backwards compatibility - disk has only its name",
				inventory: models.Inventory{Disks: []*models.Disk{
					{
						Name: name,
					},
				}},
				expectedId: path,
			},
		} {
			test := test
			It(test.testName, func() {
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusDiscovering)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(test.inventory.Disks)
				inventoryStr, err := common.MarshalInventory(&test.inventory)
				Expect(err).ToNot(HaveOccurred())
				Expect(hapi.(*Manager).UpdateInventory(ctx, &host, inventoryStr)).ToNot(HaveOccurred())
				h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
				Expect(h.Inventory).To(Not(BeEmpty()))
				inventory, err := common.UnmarshalInventory(h.Inventory)
				Expect(err).ToNot(HaveOccurred())
				Expect(inventory).To(Not(BeNil()))
				Expect(inventory.Disks).Should(HaveLen(1))
				Expect(inventory.Disks[0].ID).To(Equal(test.expectedId))
			})
		}
	})

	Context("Check populate disk eligibility", func() {
		for _, test := range []struct {
			testName         string
			agentDecision    bool
			agentReasons     []string
			serviceReasons   []string
			expectedDecision bool
			expectedReasons  []string
		}{
			{testName: "Old agent - backwards compatibility - service thinks disk is eligible",
				agentDecision: false, agentReasons: []string{},
				serviceReasons:   []string{},
				expectedDecision: true, expectedReasons: []string{}},
			{testName: "Old agent - backwards compatibility - service thinks disk is ineligible",
				agentDecision: false, agentReasons: []string{},
				serviceReasons:   []string{"Service reason"},
				expectedDecision: false, expectedReasons: []string{"Service reason"}},
			{testName: "Agent eligible, service eligible",
				agentDecision: true, agentReasons: []string{},
				serviceReasons:   []string{},
				expectedDecision: true, expectedReasons: []string{}},
			{testName: "Agent eligible, service ineligible",
				agentDecision: true, agentReasons: []string{},
				serviceReasons:   []string{"Service reason"},
				expectedDecision: false, expectedReasons: []string{"Service reason"}},
			{testName: "Agent ineligible, service eligible",
				agentDecision: false, agentReasons: []string{"Agent reason"},
				serviceReasons:   []string{},
				expectedDecision: false, expectedReasons: []string{"Agent reason"}},
			{testName: "Agent ineligible, service ineligible",
				agentDecision: false, agentReasons: []string{"Agent reason"},
				serviceReasons:   []string{"Service reason"},
				expectedDecision: false, expectedReasons: []string{"Agent reason", "Service reason"}},
			{testName: "Agent eligible with reasons, service ineligible",
				agentDecision: true, agentReasons: []string{"Agent reason"},
				serviceReasons:   []string{"Service reason"},
				expectedDecision: false, expectedReasons: []string{"Agent reason", "Service reason"}},
			{testName: "Agent eligible with reasons, service eligible",
				agentDecision: true, agentReasons: []string{"Agent reason"},
				serviceReasons:   []string{},
				expectedDecision: false, expectedReasons: []string{"Agent reason"}},
		} {
			test := test
			It(test.testName, func() {
				mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(append(test.agentReasons, test.serviceReasons...), nil)

				testInventory := models.Inventory{Disks: []*models.Disk{
					{InstallationEligibility: models.DiskInstallationEligibility{
						Eligible: test.agentDecision, NotEligibleReasons: test.agentReasons},
					},
				}}

				_, err := json.Marshal(&testInventory)
				Expect(err).ToNot(HaveOccurred())

				expectedDisks := []*models.Disk{
					{InstallationEligibility: models.DiskInstallationEligibility{
						Eligible: test.expectedDecision, NotEligibleReasons: test.expectedReasons}},
				}

				Expect(hapi.(*Manager).populateDisksEligibility(ctx, &testInventory, nil, nil, nil)).ShouldNot(HaveOccurred())
				Expect(testInventory.Disks).Should(Equal(expectedDisks))
			})
		}
	})

	Context("Check match bootstrap host MAC", func() {
		for _, test := range []struct {
			testName    string
			expectMatch bool
			hostMAC     string
		}{
			{
				testName:    "matching host MAC",
				expectMatch: true,
				hostMAC:     "50:00:00:01:02:03",
			},
			{
				testName:    "non-matching host MAC",
				expectMatch: false,
				hostMAC:     "50:00:00:0a:0b:0c",
			},
			{
				testName:    "unspecified host MAC",
				expectMatch: false,
				hostMAC:     "",
			},
		} {
			test := test
			It(test.testName, func() {

				hapi.(*Manager).Config.BootstrapHostMAC = test.hostMAC
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusDiscovering)
				inventory := models.Inventory{
					Disks: []*models.Disk{
						{
							Name:   "name",
							ByPath: "/dev/disk/by-path/name",
						},
					},
					Interfaces: []*models.Interface{
						{
							MacAddress: "52:00:00:01:02:03",
						},
						{
							MacAddress: "50:00:00:01:02:03",
						},
						{
							MacAddress: "",
						},
					},
				}

				mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(inventory.Disks)
				if test.expectMatch {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostBootstrapSetEventName),
						eventstest.WithHostIdMatcher(hostId.String()),
						eventstest.WithInfraEnvIdMatcher(infraEnvId.String())))
				}

				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				inventoryStr, err := common.MarshalInventory(&inventory)
				Expect(err).ToNot(HaveOccurred())
				Expect(hapi.(*Manager).UpdateInventory(ctx, &host, inventoryStr)).ToNot(HaveOccurred())

				h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
				Expect(h.Bootstrap).Should(Equal(test.expectMatch))
			})
		}
	})

	Context("Update inventory - no cluster", func() {
		const (
			diskName = "FirstDisk"
			diskId   = "/dev/disk/by-id/FirstDisk"
			diskPath = "/dev/FirstDisk"
		)

		BeforeEach(func() {
			host = hostutil.GenerateTestHost(hostId, infraEnvId, "", models.HostStatusDiscovering)
			host.Inventory = common.GenerateTestDefaultInventory()
			host.InstallationDiskPath = ""
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})
		It("Happy flow", func() {
			mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
			mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(
				[]*models.Disk{{ID: diskId, Name: diskName}},
			)
			Expect(hapi.UpdateInventory(ctx, &host, host.Inventory)).ToNot(HaveOccurred())

			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(h.Inventory).ToNot(BeEmpty())
			Expect(h.InstallationDiskPath).To(Equal(diskPath))
			Expect(h.InstallationDiskID).To(Equal(diskId))
		})
	})

	Context("Test update default installation disk", func() {
		const (
			diskName = "FirstDisk"
			diskId   = "/dev/disk/by-id/FirstDisk"
			diskPath = "/dev/FirstDisk"
		)

		BeforeEach(func() {
			host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusDiscovering)
			host.Inventory = common.GenerateTestDefaultInventory()
			host.InstallationDiskPath = ""
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

		})

		It("Make sure UpdateInventory updates the db", func() {
			mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(
				[]*models.Disk{{ID: diskId, Name: diskName}},
			)

			Expect(hapi.UpdateInventory(ctx, &host, host.Inventory)).ToNot(HaveOccurred())

			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(h.InstallationDiskPath).To(Equal(diskPath))
			Expect(h.InstallationDiskID).To(Equal(diskId))

			// Now make sure it gets removed if the disk is no longer in the inventory
			mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
			mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(
				[]*models.Disk{},
			)
			Expect(hapi.UpdateInventory(ctx, &host, host.Inventory)).ToNot(HaveOccurred())

			h = hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(h.InstallationDiskPath).To(Equal(""))
			Expect(h.InstallationDiskID).To(Equal(""))
		})

		It("Upgrade installation_disk_id after getting new inventory", func() {
			mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(
				[]*models.Disk{{Name: diskName}},
			)
			Expect(hapi.UpdateInventory(ctx, &host, host.Inventory)).ToNot(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(h.InstallationDiskPath).To(Equal(diskPath))
			Expect(h.InstallationDiskID).To(Equal(diskPath))

			mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(
				[]*models.Disk{{ID: diskId, Name: diskName}},
			)
			mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
			Expect(hapi.UpdateInventory(ctx, &host, host.Inventory)).ToNot(HaveOccurred())
			h = hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(h.InstallationDiskPath).To(Equal(diskPath))
			Expect(h.InstallationDiskID).To(Equal(diskId))
		})
	})
})

var _ = Describe("Update hostname", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		dbName                        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		hapi = NewManager(common.GetTestLog(), db, nil, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("set hostname", func() {
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(h.RequestedHostname).To(Equal("my-hostname"))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(h.RequestedHostname).To(Equal(""))
		}

		tests := []struct {
			name       string
			srcState   string
			validation func(error)
		}{
			{
				name:       models.HostStatusKnown,
				srcState:   models.HostStatusKnown,
				validation: success,
			},
			{
				name:       models.HostStatusDisconnected,
				srcState:   models.HostStatusDisconnected,
				validation: success,
			},
			{
				name:       models.HostStatusDiscovering,
				srcState:   models.HostStatusDiscovering,
				validation: success,
			},
			{
				name:       models.HostStatusError,
				srcState:   models.HostStatusError,
				validation: failure,
			},
			{
				name:       models.HostStatusInstalled,
				srcState:   models.HostStatusInstalled,
				validation: failure,
			},
			{
				name:       models.HostStatusInstalling,
				srcState:   models.HostStatusInstalling,
				validation: failure,
			},
			{
				name:       models.HostStatusInstallingInProgress,
				srcState:   models.HostStatusInstallingInProgress,
				validation: failure,
			},
			{
				name:       models.HostStatusResettingPendingUserAction,
				srcState:   models.HostStatusResettingPendingUserAction,
				validation: failure,
			},
			{
				name:       models.HostStatusInsufficient,
				srcState:   models.HostStatusInsufficient,
				validation: success,
			},
			{
				name:       models.HostStatusResetting,
				srcState:   models.HostStatusResetting,
				validation: failure,
			},
			{
				name:       models.HostStatusPendingForInput,
				srcState:   models.HostStatusPendingForInput,
				validation: success,
			},
			{
				name:       models.HostStatusBinding,
				srcState:   models.HostStatusBinding,
				validation: success,
			},
			{
				name:       models.HostStatusDiscoveringUnbound,
				srcState:   models.HostStatusDiscoveringUnbound,
				validation: success,
			},
			{
				name:       models.HostStatusInsufficientUnbound,
				srcState:   models.HostStatusInsufficientUnbound,
				validation: success,
			},
			{
				name:       models.HostStatusDisconnectedUnbound,
				srcState:   models.HostStatusDisconnectedUnbound,
				validation: success,
			},
			{
				name:       models.HostStatusKnownUnbound,
				srcState:   models.HostStatusKnownUnbound,
				validation: success,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, t.srcState)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				t.validation(hapi.UpdateHostname(ctx, &host, "my-hostname", db))
			})
		}
	})
})

var _ = Describe("Bind host", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		dbName                        string
		mockEvents                    *eventsapi.MockHandler
		ctrl                          *gomock.Controller
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("Bind host", func() {
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(*h.ClusterID).To(Equal(clusterId))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(h.ClusterID.String()).To(Equal(""))
		}

		tests := []struct {
			name       string
			srcState   string
			validation func(error)
		}{
			{
				name:       models.HostStatusKnown,
				srcState:   models.HostStatusKnown,
				validation: failure,
			},
			{
				name:       models.HostStatusDisconnected,
				srcState:   models.HostStatusDisconnected,
				validation: failure,
			},
			{
				name:       models.HostStatusDiscovering,
				srcState:   models.HostStatusDiscovering,
				validation: failure,
			},
			{
				name:       models.HostStatusError,
				srcState:   models.HostStatusError,
				validation: failure,
			},
			{
				name:       models.HostStatusInstalled,
				srcState:   models.HostStatusInstalled,
				validation: failure,
			},
			{
				name:       models.HostStatusInstalling,
				srcState:   models.HostStatusInstalling,
				validation: failure,
			},
			{
				name:       models.HostStatusInstallingInProgress,
				srcState:   models.HostStatusInstallingInProgress,
				validation: failure,
			},
			{
				name:       models.HostStatusResettingPendingUserAction,
				srcState:   models.HostStatusResettingPendingUserAction,
				validation: failure,
			},
			{
				name:       models.HostStatusInsufficient,
				srcState:   models.HostStatusInsufficient,
				validation: failure,
			},
			{
				name:       models.HostStatusResetting,
				srcState:   models.HostStatusResetting,
				validation: failure,
			},
			{
				name:       models.HostStatusPendingForInput,
				srcState:   models.HostStatusPendingForInput,
				validation: failure,
			},
			{
				name:       models.HostStatusKnownUnbound,
				srcState:   models.HostStatusKnownUnbound,
				validation: success,
			},
			{
				name:       models.HostStatusDisconnectedUnbound,
				srcState:   models.HostStatusDisconnectedUnbound,
				validation: failure,
			},
			{
				name:       models.HostStatusInsufficientUnbound,
				srcState:   models.HostStatusInsufficientUnbound,
				validation: failure,
			},
			{
				name:       models.HostStatusDiscoveringUnbound,
				srcState:   models.HostStatusDiscoveringUnbound,
				validation: failure,
			},
			{
				name:       models.HostStatusUnbinding,
				srcState:   models.HostStatusUnbinding,
				validation: failure,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
					eventstest.WithHostIdMatcher(hostId.String()),
					eventstest.WithInfraEnvIdMatcher(infraEnvId.String()),
					eventstest.WithClusterIdMatcher(clusterId.String()),
					eventstest.WithSeverityMatcher(models.EventSeverityInfo))).Times(1)
				host = hostutil.GenerateTestHost(hostId, infraEnvId, "", t.srcState)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				t.validation(hapi.BindHost(ctx, &host, clusterId, db))
			})
		}
	})
})

var _ = Describe("Unbind host", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		dbName                        string
		mockEvents                    *eventsapi.MockHandler
		ctrl                          *gomock.Controller
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("Unbind host", func() {
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(h.ClusterID).To(BeNil())
			Expect(swag.StringValue(h.Kind)).To(Equal(models.HostKindHost))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(*h.ClusterID).To(Equal(clusterId))
		}

		tests := []struct {
			name       string
			srcState   string
			kind       *string
			validation func(error)
		}{
			{
				name:       models.HostStatusKnown,
				srcState:   models.HostStatusKnown,
				validation: success,
			},
			{
				name:       models.HostStatusDisconnected,
				srcState:   models.HostStatusDisconnected,
				validation: success,
			},
			{
				name:       models.HostStatusDiscovering,
				srcState:   models.HostStatusDiscovering,
				validation: success,
			},
			{
				name:       models.HostStatusError,
				srcState:   models.HostStatusError,
				validation: success,
			},
			{
				name:       models.HostStatusInstalled,
				srcState:   models.HostStatusInstalled,
				validation: success,
			},
			{
				name:       models.HostStatusInstalling,
				srcState:   models.HostStatusInstalling,
				validation: failure,
			},
			{
				name:       models.HostStatusInstallingInProgress,
				srcState:   models.HostStatusInstallingInProgress,
				validation: failure,
			},
			{
				name:       models.HostStatusResettingPendingUserAction,
				srcState:   models.HostStatusResettingPendingUserAction,
				validation: failure,
			},
			{
				name:       models.HostStatusInsufficient,
				srcState:   models.HostStatusInsufficient,
				kind:       swag.String(models.HostKindAddToExistingClusterHost),
				validation: success,
			},
			{
				name:       models.HostStatusResetting,
				srcState:   models.HostStatusResetting,
				validation: failure,
			},
			{
				name:       models.HostStatusPendingForInput,
				srcState:   models.HostStatusPendingForInput,
				validation: success,
			},
			{
				name:       models.HostStatusKnownUnbound,
				srcState:   models.HostStatusKnownUnbound,
				validation: failure,
			},
			{
				name:       models.HostStatusDisconnectedUnbound,
				srcState:   models.HostStatusDisconnectedUnbound,
				validation: failure,
			},
			{
				name:       models.HostStatusInsufficientUnbound,
				srcState:   models.HostStatusInsufficientUnbound,
				validation: failure,
			},
			{
				name:       models.HostStatusDiscoveringUnbound,
				srcState:   models.HostStatusDiscoveringUnbound,
				validation: failure,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
					eventstest.WithHostIdMatcher(hostId.String()),
					eventstest.WithInfraEnvIdMatcher(infraEnvId.String()),
					eventstest.WithClusterIdMatcher(swag.StringValue(nil)),
					eventstest.WithSeverityMatcher(models.EventSeverityInfo))).Times(1)
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, t.srcState)
				if t.kind != nil {
					host.Kind = t.kind
				}
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				t.validation(hapi.UnbindHost(ctx, &host, db))
			})
		}
	})
})

var _ = Describe("Update disk installation path", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		ctrl                          *gomock.Controller
		mockValidator                 *hardware.MockValidator
		dbName                        string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		leader := &leader.DummyElector{}
		mockValidator = hardware.NewMockValidator(ctrl)
		logger := common.GetTestLog()
		hapi = NewManager(logger, db, nil, mockValidator, nil, createValidatorCfg(), nil, defaultConfig, leader, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	success := func(reply error) {
		Expect(reply).To(BeNil())
		h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
		Expect(h.InstallationDiskID).To(Equal(common.TestDiskId))
		Expect(h.InstallationDiskPath).To(Equal(common.TestDiskPath))
	}

	failure := func(reply error) {
		Expect(reply).To(HaveOccurred())
		h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
		Expect(h.InstallationDiskID).To(Equal(""))
		Expect(h.InstallationDiskPath).To(Equal(""))
	}

	Context("validate disk installation path", func() {
		It("illegal disk installation path", func() {
			host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusKnown)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return([]*models.Disk{common.TestDefaultConfig.Disks}, nil)
			failure(hapi.UpdateInstallationDisk(ctx, db, &host, "/no/such/disk"))
		})
		//happy flow is validated implicitly in the next test context
	})

	Context("validate get host valid disks error", func() {
		It("get host valid disks returns an error", func() {
			host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusKnown)
			expectedError := errors.New("bad inventory")
			mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return([]*models.Disk{common.TestDefaultConfig.Disks}, expectedError)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			err := hapi.UpdateInstallationDisk(ctx, db, &host, "/no/such/disk")
			Expect(err).To(Equal(expectedError))
			failure(err)
		})
		//happy flow is validated implicitly in the next test context
	})

	Context("validate host state before updating disk installation path", func() {
		tests := []struct {
			name       string
			srcState   string
			validation bool
		}{
			{
				name:       models.HostStatusKnown,
				srcState:   models.HostStatusKnown,
				validation: true,
			},
			{
				name:       models.HostStatusDisconnected,
				srcState:   models.HostStatusDisconnected,
				validation: true,
			},
			{
				name:       models.HostStatusDiscovering,
				srcState:   models.HostStatusDiscovering,
				validation: true,
			},
			{
				name:       models.HostStatusError,
				srcState:   models.HostStatusError,
				validation: false,
			},
			{
				name:       models.HostStatusInstalled,
				srcState:   models.HostStatusInstalled,
				validation: false,
			},
			{
				name:       models.HostStatusInstalling,
				srcState:   models.HostStatusInstalling,
				validation: false,
			},
			{
				name:       models.HostStatusInstallingInProgress,
				srcState:   models.HostStatusInstallingInProgress,
				validation: false,
			},
			{
				name:       models.HostStatusResettingPendingUserAction,
				srcState:   models.HostStatusResettingPendingUserAction,
				validation: false,
			},
			{
				name:       models.HostStatusInsufficient,
				srcState:   models.HostStatusInsufficient,
				validation: true,
			},
			{
				name:       models.HostStatusResetting,
				srcState:   models.HostStatusResetting,
				validation: false,
			},
			{
				name:       models.HostStatusPendingForInput,
				srcState:   models.HostStatusPendingForInput,
				validation: true,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				if t.validation {
					mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return([]*models.Disk{common.TestDefaultConfig.Disks}, nil).AnyTimes()
				}

				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, t.srcState)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				err := hapi.UpdateInstallationDisk(ctx, db, &host, common.TestDiskId)

				if t.validation {
					success(err)
				} else {
					failure(err)
				}
			})
		}
	})
})

var _ = Describe("SetBootstrap", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		ctrl                          *gomock.Controller
		mockEvents                    *eventsapi.MockHandler
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		dbName                        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		hapi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())

		host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusResetting)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		h := hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
		Expect(h.Bootstrap).Should(Equal(false))
	})

	tests := []struct {
		IsBootstrap bool
	}{
		{
			IsBootstrap: true,
		},
		{
			IsBootstrap: false,
		},
	}

	for i := range tests {
		t := tests[i]
		It(fmt.Sprintf("Boostrap %s", strconv.FormatBool(t.IsBootstrap)), func() {
			if t.IsBootstrap {
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostBootstrapSetEventName),
					eventstest.WithHostIdMatcher(hostId.String()),
					eventstest.WithInfraEnvIdMatcher(infraEnvId.String())))
			}
			Expect(hapi.SetBootstrap(ctx, &host, t.IsBootstrap, db)).ShouldNot(HaveOccurred())

			h := hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
			Expect(h.Bootstrap).Should(Equal(t.IsBootstrap))
		})
	}

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("UpdateNTP", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		ctrl                          *gomock.Controller
		mockEvents                    *eventsapi.MockHandler
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		dbName                        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		hapi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())

		host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusResetting)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		h := hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
		Expect(h.NtpSources).Should(BeEmpty())
	})

	tests := []struct {
		name       string
		ntpSources []*models.NtpSource
	}{
		{
			name:       "empty NTP sources",
			ntpSources: []*models.NtpSource{},
		},
		{
			name:       "one NTP source",
			ntpSources: []*models.NtpSource{common.TestNTPSourceSynced},
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			Expect(hapi.UpdateNTP(ctx, &host, t.ntpSources, db)).ShouldNot(HaveOccurred())

			h := hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)

			marshalled, err := json.Marshal(t.ntpSources)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(h.NtpSources).Should(Equal(string(marshalled)))
		})
	}

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("UpdateMachineConfigPoolName", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		ctrl                          *gomock.Controller
		mockEvents                    *eventsapi.MockHandler
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		dbName                        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		hapi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
	})

	tests := []struct {
		name    string
		day2    bool
		status  string
		isValid bool
	}{
		{
			name:    "day1",
			status:  models.HostStatusDiscovering,
			day2:    false,
			isValid: true,
		},
		{
			name:    "day2_before_installation",
			status:  models.HostStatusDiscovering,
			day2:    true,
			isValid: true,
		},
		{
			name:    "day2_after_installation",
			status:  models.HostStatusInstalled,
			day2:    true,
			isValid: false,
		},
		{
			name:    "day2_binding",
			status:  models.HostStatusBinding,
			day2:    true,
			isValid: true,
		},
		{
			name:    "day2_unbound_discoverying",
			status:  models.HostStatusDiscoveringUnbound,
			day2:    true,
			isValid: true,
		},
		{
			name:    "day2_unbound_insufficient",
			status:  models.HostStatusInsufficientUnbound,
			day2:    true,
			isValid: true,
		},
		{
			name:    "day2_unbound_disconnected",
			status:  models.HostStatusDisconnectedUnbound,
			day2:    true,
			isValid: true,
		},
		{
			name:    "day2_unbound_known",
			status:  models.HostStatusKnownUnbound,
			day2:    true,
			isValid: true,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			// Setup
			if t.day2 {
				host = hostutil.GenerateTestHostAddedToCluster(hostId, infraEnvId, clusterId, t.status)
			} else {
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, t.status)
			}

			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

			h := hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
			Expect(h.MachineConfigPoolName).Should(BeEmpty())

			// Test
			err := hapi.UpdateMachineConfigPoolName(ctx, db, &host, t.name)
			h = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)

			if t.isValid {
				Expect(err).ShouldNot(HaveOccurred())
				Expect(h.MachineConfigPoolName).Should(Equal(t.name))
			} else {
				Expect(err).Should(HaveOccurred())
				Expect(h.MachineConfigPoolName).Should(BeEmpty())
			}

		})
	}

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("UpdateIgnitionEndpointToken", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		ctrl                          *gomock.Controller
		mockEvents                    *eventsapi.MockHandler
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		dbName                        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		hapi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
	})

	tests := []struct {
		name    string
		day2    bool
		status  string
		isValid bool
	}{
		{
			name:    "day1_before_installation",
			status:  models.HostStatusDiscovering,
			day2:    false,
			isValid: true,
		},
		{
			name:    "day1_after_installation",
			status:  models.HostStatusInstalling,
			day2:    false,
			isValid: false,
		},
		{
			name:    "day2_before_installation",
			status:  models.HostStatusDiscovering,
			day2:    true,
			isValid: true,
		},
		{
			name:    "day2_after_installation",
			status:  models.HostStatusInstalled,
			day2:    true,
			isValid: false,
		},
		{
			name:    "day2_binding",
			status:  models.HostStatusBinding,
			day2:    true,
			isValid: true,
		},
		{
			name:    "day2_unbound_discoverying",
			status:  models.HostStatusDiscoveringUnbound,
			day2:    true,
			isValid: true,
		},
		{
			name:    "day2_unbound_insufficient",
			status:  models.HostStatusInsufficientUnbound,
			day2:    true,
			isValid: true,
		},
		{
			name:    "day2_unbound_disconnected",
			status:  models.HostStatusDisconnectedUnbound,
			day2:    true,
			isValid: true,
		},
		{
			name:    "day2_unbound_known",
			status:  models.HostStatusKnownUnbound,
			day2:    true,
			isValid: true,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			// Setup
			if t.day2 {
				host = hostutil.GenerateTestHostAddedToCluster(hostId, infraEnvId, clusterId, t.status)
			} else {
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, t.status)
			}

			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

			h := hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
			Expect(h.IgnitionEndpointToken).Should(BeEquivalentTo(""))
			Expect(h.IgnitionEndpointTokenSet).Should(BeEquivalentTo(false))

			// Test
			err := hapi.UpdateIgnitionEndpointToken(ctx, db, &host, t.name)
			h = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)

			if t.isValid {
				Expect(err).ShouldNot(HaveOccurred())
				Expect(h.IgnitionEndpointToken).Should(Equal(t.name))
				Expect(h.IgnitionEndpointTokenSet).Should(BeEquivalentTo(true))
			} else {
				Expect(err).Should(HaveOccurred())
				Expect(h.IgnitionEndpointToken).Should(BeEquivalentTo(""))
				Expect(h.IgnitionEndpointTokenSet).Should(BeEquivalentTo(false))
			}

		})
	}

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("update logs_info", func() {
	var (
		ctx                           = context.Background()
		ctrl                          *gomock.Controller
		db                            *gorm.DB
		hapi                          API
		host                          models.Host
		hostId, clusterId, infraEnvId strfmt.UUID
		dbName                        string
	)

	BeforeEach(func() {
		dummy := &leader.DummyElector{}
		db, dbName = common.PrepareTestDB()
		mockOperators := operators.NewMockAPI(ctrl)
		hapi = NewManager(common.GetTestLog(), db, nil, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, mockOperators, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusInstallingInProgress)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	validateLogsStartedAt := func(h *models.Host) {
		Expect(time.Time(h.LogsStartedAt).Equal(time.Time{})).To(BeFalse())
	}

	validateCollectedAtNotUpdated := func(h *models.Host) {
		Expect(time.Time(h.LogsStartedAt).Equal(time.Time{})).To(BeTrue())
	}

	tests := []struct {
		name              string
		logsInfo          models.LogsState
		validateTimestamp func(h *models.Host)
	}{
		{
			name:              "log collection started",
			logsInfo:          models.LogsStateRequested,
			validateTimestamp: validateLogsStartedAt,
		},
		{
			name:              "log collecting",
			logsInfo:          models.LogsStateCollecting,
			validateTimestamp: validateCollectedAtNotUpdated,
		},
		{
			name:              "log collecting completed",
			logsInfo:          models.LogsStateCompleted,
			validateTimestamp: validateCollectedAtNotUpdated,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			err := hapi.UpdateLogsProgress(ctx, &host, string(t.logsInfo))
			Expect(err).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(h.LogsInfo).To(Equal(t.logsInfo))
			t.validateTimestamp(&h.Host)
		})
	}
})

var _ = Describe("UpdateImageStatus", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		ctrl                          *gomock.Controller
		mockEvents                    *eventsapi.MockHandler
		mockMetric                    *metrics.MockAPI
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		dbName                        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		hapi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), mockMetric, defaultConfig, dummy, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())

		host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusResetting)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		h := hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
		Expect(h.ImagesStatus).Should(BeEmpty())
	})

	var testAlreadyPulledImageStatuses = &models.ContainerImageAvailability{
		Name:         "image",
		Result:       models.ContainerImageAvailabilityResultSuccess,
		SizeBytes:    0.0,
		Time:         0.0,
		DownloadRate: 0.0,
	}

	tests := []struct {
		name string

		originalImageStatuses common.ImageStatuses
		newImageStatus        *models.ContainerImageAvailability
		changeInDB            bool
	}{
		{
			name:                  "no images - new success",
			originalImageStatuses: common.ImageStatuses{},
			newImageStatus:        common.TestImageStatusesSuccess,
			changeInDB:            true,
		},
		{
			name:                  "no images - new failure",
			originalImageStatuses: common.ImageStatuses{},
			newImageStatus:        common.TestImageStatusesSuccess,
			changeInDB:            true,
		},
		{
			name:                  "original success - new success",
			originalImageStatuses: common.ImageStatuses{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
			newImageStatus:        testAlreadyPulledImageStatuses,
			changeInDB:            false,
		},
		{
			name:                  "original success - new already pulled",
			originalImageStatuses: common.ImageStatuses{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
			newImageStatus:        testAlreadyPulledImageStatuses,
			changeInDB:            false,
		},
		{
			name:                  "original success - new failure",
			originalImageStatuses: common.ImageStatuses{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
			newImageStatus:        common.TestImageStatusesFailure,
			changeInDB:            true,
		},
		{
			name:                  "original failure - new success",
			originalImageStatuses: common.ImageStatuses{common.TestDefaultConfig.ImageName: common.TestImageStatusesFailure},
			newImageStatus:        common.TestImageStatusesSuccess,
			changeInDB:            true,
		},
		{
			name:                  "original failure - new failure",
			originalImageStatuses: common.ImageStatuses{common.TestDefaultConfig.ImageName: common.TestImageStatusesFailure},
			newImageStatus:        common.TestImageStatusesFailure,
			changeInDB:            false,
		},
		{
			name:                  "original failure - new already pulled",
			originalImageStatuses: common.ImageStatuses{common.TestDefaultConfig.ImageName: common.TestImageStatusesFailure},
			newImageStatus:        testAlreadyPulledImageStatuses,
			changeInDB:            true,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			expectedImage := &models.ContainerImageAvailability{
				Name:         t.newImageStatus.Name,
				Result:       t.newImageStatus.Result,
				DownloadRate: t.newImageStatus.DownloadRate,
				SizeBytes:    t.newImageStatus.SizeBytes,
				Time:         t.newImageStatus.Time,
			}

			if len(t.originalImageStatuses) == 0 {
				eventMsg := fmt.Sprintf("Host %s: New image status %s. result: %s.",
					hostutil.GetHostnameForMsg(&host), expectedImage.Name, expectedImage.Result)

				if expectedImage.SizeBytes > 0 {
					eventMsg += fmt.Sprintf(" time: %.2f seconds; size: %.2f Megabytes; download rate: %.2f MBps",
						expectedImage.Time, expectedImage.SizeBytes/math.Pow(1024, 2), expectedImage.DownloadRate)
				}

				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ImageStatusUpdatedEventName),
					eventstest.WithHostIdMatcher(hostId.String()),
					eventstest.WithInfraEnvIdMatcher(infraEnvId.String()),
					eventstest.WithSeverityMatcher(models.EventSeverityInfo),
					eventstest.WithMessageMatcher(eventMsg))).Times(1)

				mockMetric.EXPECT().ImagePullStatus(hostId, expectedImage.Name, string(expectedImage.Result), expectedImage.DownloadRate).Times(1)
			} else {
				expectedImage.DownloadRate = t.originalImageStatuses[common.TestDefaultConfig.ImageName].DownloadRate
				expectedImage.SizeBytes = t.originalImageStatuses[common.TestDefaultConfig.ImageName].SizeBytes
				expectedImage.Time = t.originalImageStatuses[common.TestDefaultConfig.ImageName].Time

				bytes, err := json.Marshal(t.originalImageStatuses)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(bytes).ShouldNot(BeNil())
				host.ImagesStatus = string(bytes)
			}

			Expect(hapi.UpdateImageStatus(ctx, &host, t.newImageStatus, db)).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)

			if t.changeInDB {
				var statusInDb map[string]*models.ContainerImageAvailability
				Expect(json.Unmarshal([]byte(h.ImagesStatus), &statusInDb)).ShouldNot(HaveOccurred())
				Expect(statusInDb).Should(ContainElement(expectedImage))
			}
		})
	}

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("UpdateKubeKeyNS", func() {
	var (
		ctx                           = context.Background()
		hostApi                       API
		db                            *gorm.DB
		ctrl                          *gomock.Controller
		mockEvents                    *eventsapi.MockHandler
		hostId, clusterId, infraEnvId strfmt.UUID
		dbName                        string
		host                          common.Host
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		hostApi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())

		host = common.Host{
			Host:             hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusKnown),
			KubeKeyNamespace: "namespace",
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	tests := []struct {
		name      string
		namespace string
	}{
		{
			name:      "test namespace",
			namespace: "test",
		},
		{
			name:      "same namespace",
			namespace: "namespace",
		},
		{
			name:      "empty namespace",
			namespace: "",
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			Expect(hostApi.UpdateKubeKeyNS(ctx, hostId.String(), t.namespace)).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(h.KubeKeyNamespace).Should(Equal(t.namespace))
		})
	}

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("AutoAssignRole", func() {
	var (
		ctx             = context.Background()
		clusterId       strfmt.UUID
		infraEnvId      strfmt.UUID
		hapi            API
		db              *gorm.DB
		ctrl            *gomock.Controller
		mockHwValidator *hardware.MockValidator
		mockEvents      *eventsapi.MockHandler
		dbName          string
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		mockHwValidator.EXPECT().ListEligibleDisks(gomock.Any()).AnyTimes()
		mockHwValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(nil, nil).AnyTimes()
		mockOperators := operators.NewMockAPI(ctrl)
		db, dbName = common.PrepareTestDB()
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		dummy := &leader.DummyElector{}
		hapi = NewManager(
			common.GetTestLog(),
			db,
			mockEvents,
			mockHwValidator,
			nil,
			createValidatorCfg(),
			nil,
			defaultConfig,
			dummy,
			mockOperators,
			nil,
		)
		Expect(db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterId, Kind: swag.String(models.ClusterKindCluster)}}).Error).ShouldNot(HaveOccurred())
		mockOperators.EXPECT().ValidateHost(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied)},
		}, nil)
		masterRequirements := models.ClusterHostRequirementsDetails{
			CPUCores:   4,
			DiskSizeGb: 120,
			RAMMib:     16384,
		}

		workerRequirements := models.ClusterHostRequirementsDetails{
			CPUCores:   2,
			DiskSizeGb: 120,
			RAMMib:     8192,
		}

		mockHwValidator.EXPECT().GetClusterHostRequirements(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirements, error) {
			var details models.ClusterHostRequirementsDetails
			if host.Role == models.HostRoleMaster {
				details = masterRequirements
			} else {
				details = workerRequirements
			}
			return &models.ClusterHostRequirements{Total: &details}, nil
		})
		mockHwValidator.EXPECT().GetPreflightHardwareRequirements(gomock.Any(), gomock.Any()).AnyTimes().Return(
			&models.PreflightHardwareRequirements{
				Ocp: &models.HostTypeHardwareRequirementsWrapper{
					Master: &models.HostTypeHardwareRequirements{
						Quantitative: &masterRequirements,
					},
					Worker: &models.HostTypeHardwareRequirements{
						Quantitative: &workerRequirements,
					},
				},
			}, nil)
		mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("abc").AnyTimes()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		common.CloseDB(db)
		ctrl.Finish()
	})

	mockRoleSuggestionEvent := func(h *models.Host) {
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostRoleUpdatedEventName),
			eventstest.WithHostIdMatcher(h.ID.String()),
			eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String()),
		))
	}

	verifyAutoAssignRole := func(host *models.Host, success bool, isSelected bool) {
		if isSelected {
			mockRoleSuggestionEvent(host)
		}
		selected, err := hapi.AutoAssignRole(ctx, host, db)
		Expect(selected).To(Equal(isSelected))
		if success {
			Expect(err).ShouldNot(HaveOccurred())
		} else {
			Expect(err).Should(HaveOccurred())
		}
	}

	Context("single host role selection", func() {
		tests := []struct {
			name         string
			srcRole      models.HostRole
			inventory    string
			success      bool
			selected     bool
			expectedRole models.HostRole
		}{
			{
				name:         "role already set to worker",
				srcRole:      models.HostRoleWorker,
				inventory:    hostutil.GenerateMasterInventory(),
				success:      true,
				selected:     false,
				expectedRole: models.HostRoleWorker,
			}, {
				name:         "role already set to master",
				srcRole:      models.HostRoleMaster,
				inventory:    hostutil.GenerateMasterInventory(),
				success:      true,
				selected:     false,
				expectedRole: models.HostRoleMaster,
			}, {
				name:         "no inventory",
				srcRole:      models.HostRoleAutoAssign,
				inventory:    "",
				success:      false,
				selected:     false,
				expectedRole: models.HostRoleAutoAssign,
			}, {
				name:         "auto-assign master",
				srcRole:      models.HostRoleAutoAssign,
				inventory:    hostutil.GenerateMasterInventory(),
				success:      true,
				selected:     true,
				expectedRole: models.HostRoleMaster,
			}, {
				name:         "auto-assign worker",
				srcRole:      models.HostRoleAutoAssign,
				inventory:    workerInventory(),
				success:      true,
				selected:     true,
				expectedRole: models.HostRoleWorker,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				h := hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), infraEnvId, clusterId, models.HostStatusKnown)
				h.Inventory = t.inventory
				h.Role = t.srcRole
				h.SuggestedRole = ""
				Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
				verifyAutoAssignRole(&h, t.success, t.selected)
				Expect(hostutil.GetHostFromDB(*h.ID, infraEnvId, db).Role).Should(Equal(t.expectedRole))
			})
		}
	})

	It("cluster already have enough master nodes", func() {
		for i := 0; i < common.MinMasterHostsNeededForInstallation; i++ {
			h := hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), infraEnvId, clusterId, models.HostStatusKnown)
			h.Inventory = hostutil.GenerateMasterInventory()
			h.Role = models.HostRoleAutoAssign
			h.SuggestedRole = ""
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			verifyAutoAssignRole(&h, true, true)
			Expect(hostutil.GetHostFromDB(*h.ID, infraEnvId, db).Role).Should(Equal(models.HostRoleMaster))
		}

		h := hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), infraEnvId, clusterId, models.HostStatusKnown)
		h.Inventory = hostutil.GenerateMasterInventory()
		h.Role = models.HostRoleAutoAssign
		h.SuggestedRole = ""
		Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
		verifyAutoAssignRole(&h, true, true)
		Expect(hostutil.GetHostFromDB(*h.ID, infraEnvId, db).Role).Should(Equal(models.HostRoleWorker))
	})
})

var _ = Describe("IsValidMasterCandidate", func() {
	var (
		clusterId  strfmt.UUID
		infraEnvId strfmt.UUID
		hapi       API
		db         *gorm.DB
		dbName     string
		ctrl       *gomock.Controller
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		dummy := &leader.DummyElector{}
		testLog := common.GetTestLog()
		hwValidatorCfg := createValidatorCfg()
		ctrl = gomock.NewController(GinkgoT())
		mockOperators := operators.NewMockAPI(ctrl)
		hwValidator := hardware.NewValidator(testLog, *hwValidatorCfg, mockOperators)
		mockOperators.EXPECT().GetRequirementsBreakdownForHostInCluster(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return([]*models.OperatorHostRequirements{}, nil)
		mockOperators.EXPECT().GetPreflightRequirementsBreakdownForCluster(gomock.Any(), gomock.Any()).AnyTimes().Return([]*models.OperatorHardwareRequirements{}, nil)
		hapi = NewManager(
			common.GetTestLog(),
			db,
			nil,
			hwValidator,
			nil,
			hwValidatorCfg,
			nil,
			defaultConfig,
			dummy,
			mockOperators,
			nil,
		)
		Expect(db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterId, Kind: swag.String(models.ClusterKindCluster)}}).Error).ShouldNot(HaveOccurred())
		mockOperators.EXPECT().ValidateHost(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied)},
		}, nil)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		common.CloseDB(db)
		ctrl.Finish()
	})

	Context("single host role selection", func() {
		tests := []struct {
			name      string
			srcState  string
			srcRole   models.HostRole
			inventory string
			isValid   bool
		}{
			{
				name:      "not ready host",
				srcState:  models.HostStatusPendingForInput,
				srcRole:   models.HostRoleAutoAssign,
				inventory: hostutil.GenerateMasterInventory(),
				isValid:   true,
			}, {
				name:      "role is already assigned as worker",
				srcState:  models.HostStatusKnown,
				srcRole:   models.HostRoleWorker,
				inventory: hostutil.GenerateMasterInventory(),
				isValid:   false,
			}, {
				name:      "master but insufficient hw",
				srcState:  models.HostStatusKnown,
				srcRole:   models.HostRoleMaster,
				inventory: workerInventory(),
				isValid:   false,
			}, {
				name:      "valid master",
				srcState:  models.HostStatusKnown,
				srcRole:   models.HostRoleMaster,
				inventory: hostutil.GenerateMasterInventory(),
				isValid:   true,
			}, {
				name:      "valid for master with auto-assign role",
				srcState:  models.HostStatusKnown,
				srcRole:   models.HostRoleAutoAssign,
				inventory: hostutil.GenerateMasterInventory(),
				isValid:   true,
			}, {
				name:      "worker inventory",
				srcState:  models.HostStatusKnown,
				srcRole:   models.HostRoleAutoAssign,
				inventory: workerInventory(),
				isValid:   false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				h := hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), infraEnvId, clusterId, t.srcState)
				h.Inventory = t.inventory
				h.Role = t.srcRole
				Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
				var cluster common.Cluster
				Expect(db.Preload("Hosts").Take(&cluster, "id = ?", clusterId.String()).Error).ToNot(HaveOccurred())
				isValidReply, err := hapi.IsValidMasterCandidate(&h, &cluster, db, common.GetTestLog())
				Expect(isValidReply).Should(Equal(t.isValid))
				Expect(err).ShouldNot(HaveOccurred())
			})
		}
	})
})

var _ = Describe("Validation metrics and events", func() {

	const (
		openshiftVersion = "dummyVersion"
		emailDomain      = "dummy.test"
	)

	var (
		ctrl            *gomock.Controller
		ctx             = context.Background()
		db              *gorm.DB
		dbName          string
		mockEvents      *eventsapi.MockHandler
		mockHwValidator *hardware.MockValidator
		mockMetric      *metrics.MockAPI
		validatorCfg    *hardware.ValidatorCfg
		m               *Manager
		h               *models.Host
	)

	generateTestValidationResult := func(status ValidationStatus) ValidationsStatus {
		validationRes := ValidationsStatus{
			"hw": {
				{
					ID:     HasMinCPUCores,
					Status: status,
				},
			},
		}
		return validationRes
	}

	registerTestHostWithValidations := func(infraEnvID, clusterID strfmt.UUID) *models.Host {

		hostID := strfmt.UUID(uuid.New().String())
		h := hostutil.GenerateTestHost(hostID, infraEnvID, clusterID, models.HostStatusInsufficient)

		validationRes := generateTestValidationResult(ValidationFailure)
		bytes, err := json.Marshal(validationRes)
		Expect(err).ToNot(HaveOccurred())
		h.ValidationsInfo = string(bytes)

		h.Inventory = hostutil.GenerateMasterInventory()

		err = m.RegisterHost(ctx, &h, db)
		Expect(err).ToNot(HaveOccurred())

		return &h
	}

	generateValidationCtx := func() *validationContext {
		vc := validationContext{
			cluster: &common.Cluster{
				Cluster: models.Cluster{
					OpenshiftVersion: openshiftVersion,
					EmailDomain:      emailDomain,
				},
			},
		}
		return &vc
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		validatorCfg = createValidatorCfg()
		m = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, mockMetric, defaultConfig, nil, nil, nil)
		h = registerTestHostWithValidations(strfmt.UUID(uuid.New().String()), strfmt.UUID(uuid.New().String()))
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("Test ReportValidationFailedMetrics", func() {

		mockMetric.EXPECT().HostValidationFailed(openshiftVersion, emailDomain, models.HostValidationIDHasMinCPUCores)

		err := m.ReportValidationFailedMetrics(ctx, h, openshiftVersion, emailDomain)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Test reportValidationStatusChanged", func() {

		mockEvents.EXPECT().SendHostEvent(ctx, eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostValidationFixedEventName),
			eventstest.WithHostIdMatcher(h.ID.String()),
			eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String())))

		vc := generateValidationCtx()
		newValidationRes := generateTestValidationResult(ValidationSuccess)
		var currentValidationRes ValidationsStatus
		err := json.Unmarshal([]byte(h.ValidationsInfo), &currentValidationRes)
		Expect(err).ToNot(HaveOccurred())
		m.reportValidationStatusChanged(ctx, vc, h, newValidationRes, currentValidationRes)

		mockMetric.EXPECT().HostValidationChanged(openshiftVersion, emailDomain, models.HostValidationIDHasMinCPUCores)
		mockEvents.EXPECT().SendHostEvent(ctx, eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostValidationFailedEventName),
			eventstest.WithHostIdMatcher(h.ID.String()),
			eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String())))

		currentValidationRes = newValidationRes
		newValidationRes = generateTestValidationResult(ValidationFailure)
		m.reportValidationStatusChanged(ctx, vc, h, newValidationRes, currentValidationRes)
	})

	It("Test reportValidationStatusChanged for unbound host", func() {

		mockEvents.EXPECT().SendHostEvent(ctx, eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostValidationFixedEventName),
			eventstest.WithHostIdMatcher(h.ID.String()),
			eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String())))

		vc := generateValidationCtx()
		newValidationRes := generateTestValidationResult(ValidationSuccess)
		var currentValidationRes ValidationsStatus
		err := json.Unmarshal([]byte(h.ValidationsInfo), &currentValidationRes)
		Expect(err).ToNot(HaveOccurred())
		m.reportValidationStatusChanged(ctx, vc, h, newValidationRes, currentValidationRes)

		mockMetric.EXPECT().HostValidationChanged(openshiftVersion, emailDomain, models.HostValidationIDHasMinCPUCores)
		mockEvents.EXPECT().SendHostEvent(ctx, eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostValidationFailedEventName),
			eventstest.WithHostIdMatcher(h.ID.String()),
			eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String())))

		currentValidationRes = newValidationRes
		newValidationRes = generateTestValidationResult(ValidationFailure)
		h.ClusterID = nil
		m.reportValidationStatusChanged(ctx, vc, h, newValidationRes, currentValidationRes)
	})
})

var _ = Describe("SetDiskSpeed", func() {
	var (
		ctrl            *gomock.Controller
		ctx             = context.Background()
		db              *gorm.DB
		dbName          string
		mockEvents      *eventsapi.MockHandler
		mockHwValidator *hardware.MockValidator
		validatorCfg    *hardware.ValidatorCfg
		m               *Manager
		h               *models.Host
	)

	registerTestHost := func(infraEnvID, clusterID strfmt.UUID) *models.Host {

		hostID := strfmt.UUID(uuid.New().String())
		h := hostutil.GenerateTestHost(hostID, infraEnvID, clusterID, models.HostStatusInsufficient)

		h.Inventory = hostutil.GenerateMasterInventory()

		Expect(m.RegisterHost(ctx, &h, db)).ToNot(HaveOccurred())

		return &h
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		validatorCfg = createValidatorCfg()
		m = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, nil, defaultConfig, nil, nil, nil)
		h = registerTestHost(strfmt.UUID(uuid.New().String()), strfmt.UUID(uuid.New().String()))
	})

	verifyValidResult := func(h *models.Host, path string, exitCode int64, speedMs int64) {
		diskInfo, err := common.GetDiskInfo(h.DisksInfo, path)
		Expect(err).ToNot(HaveOccurred())
		Expect(diskInfo).ToNot(BeNil())
		Expect(diskInfo.DiskSpeed).ToNot(BeNil())
		Expect(diskInfo.DiskSpeed.Tested).To(BeTrue())
		Expect(diskInfo.DiskSpeed.ExitCode).To(Equal(exitCode))
		if exitCode == 0 {
			Expect(diskInfo.DiskSpeed.SpeedMs).To(Equal(speedMs))
		}
	}

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("Happy flow", func() {
		err := m.SetDiskSpeed(ctx, h, "/dev/sda", 2, 0, nil)
		Expect(err).ToNot(HaveOccurred())
		var newHost models.Host
		Expect(db.Take(&newHost, "id = ? and cluster_id = ?", h.ID.String(), h.ClusterID.String()).Error).ToNot(HaveOccurred())
		verifyValidResult(&newHost, "/dev/sda", 0, 2)
	})

	It("Two disks", func() {
		Expect(m.SetDiskSpeed(ctx, h, "/dev/sda", 2, 0, nil)).ToNot(HaveOccurred())
		Expect(m.SetDiskSpeed(ctx, h, "/dev/sdb", 4, 0, nil)).ToNot(HaveOccurred())
		var newHost models.Host
		Expect(db.Take(&newHost, "id = ? and cluster_id = ?", h.ID.String(), h.ClusterID.String()).Error).ToNot(HaveOccurred())
		verifyValidResult(&newHost, "/dev/sda", 0, 2)
		verifyValidResult(&newHost, "/dev/sdb", 0, 4)
	})

	It("One in error", func() {
		Expect(m.SetDiskSpeed(ctx, h, "/dev/sda", 2, 5, nil)).ToNot(HaveOccurred())
		Expect(m.SetDiskSpeed(ctx, h, "/dev/sdb", 4, 0, nil)).ToNot(HaveOccurred())
		var newHost models.Host
		Expect(db.Take(&newHost, "id = ? and cluster_id = ?", h.ID.String(), h.ClusterID.String()).Error).ToNot(HaveOccurred())
		verifyValidResult(&newHost, "/dev/sda", 5, 2)
		verifyValidResult(&newHost, "/dev/sdb", 0, 4)
	})
	It("One missing", func() {
		err := m.SetDiskSpeed(ctx, h, "/dev/sda", 2, 0, nil)
		Expect(err).ToNot(HaveOccurred())
		var newHost models.Host
		Expect(db.Take(&newHost, "id = ? and cluster_id = ?", h.ID.String(), h.ClusterID.String()).Error).ToNot(HaveOccurred())
		verifyValidResult(&newHost, "/dev/sda", 0, 2)
		diskInfo, err := common.GetDiskInfo(h.DisksInfo, "/dev/sdb")
		Expect(err).ToNot(HaveOccurred())
		Expect(diskInfo).To(BeNil())
	})

})

var _ = Describe("ResetHostValidation", func() {
	var (
		ctrl            *gomock.Controller
		ctx             = context.Background()
		db              *gorm.DB
		dbName          string
		mockEvents      *eventsapi.MockHandler
		mockHwValidator *hardware.MockValidator
		validatorCfg    *hardware.ValidatorCfg
		m               *Manager
		h               *models.Host
	)

	registerTestHost := func(infraEnvID strfmt.UUID, clusterID *strfmt.UUID) *models.Host {

		hostID := strfmt.UUID(uuid.New().String())
		var h models.Host
		if clusterID == nil {
			h = hostutil.GenerateUnassignedTestHost(hostID, infraEnvID, models.HostStatusInsufficient)
		} else {
			h = hostutil.GenerateTestHost(hostID, infraEnvID, *clusterID, models.HostStatusInsufficient)
		}

		h.Inventory = hostutil.GenerateMasterInventory()
		h.InstallationDiskID = "/dev/sda"

		Expect(m.RegisterHost(ctx, &h, db)).ToNot(HaveOccurred())

		return &h
	}

	registerUnassignedTestHost := func(infraEnvID strfmt.UUID) *models.Host {
		return registerTestHost(infraEnvID, nil)
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		validatorCfg = createValidatorCfg()
		mockMetric := metrics.NewMockAPI(ctrl)
		clusterId := strfmt.UUID(uuid.New().String())
		m = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, mockMetric, defaultConfig, nil, nil, nil)
		h = registerTestHost(strfmt.UUID(uuid.New().String()), &clusterId)
		mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("/dev/sda").AnyTimes()
		mockMetric.EXPECT().ImagePullStatus(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithHostIdMatcher(h.ID.String()),
			eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String()))).AnyTimes()
	})

	verifyExistingDiskResult := func(h *models.Host, path string, exitCode int64, speedMs int64) {
		diskInfo, err := common.GetDiskInfo(h.DisksInfo, path)
		Expect(err).ToNot(HaveOccurred())
		Expect(diskInfo).ToNot(BeNil())
		Expect(diskInfo.DiskSpeed).ToNot(BeNil())
		Expect(diskInfo.DiskSpeed.Tested).To(BeTrue())
		Expect(diskInfo.DiskSpeed.ExitCode).To(Equal(exitCode))
		if exitCode == 0 {
			Expect(diskInfo.DiskSpeed.SpeedMs).To(Equal(speedMs))
		}
	}

	verifyNonExistentDiskResult := func(h *models.Host, path string) {
		diskInfo, err := common.GetDiskInfo(h.DisksInfo, path)
		Expect(err).ToNot(HaveOccurred())
		Expect(diskInfo).ToNot(BeNil())
		Expect(diskInfo.DiskSpeed).To(BeNil())
	}

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("One disk in error", func() {
		Expect(m.SetDiskSpeed(ctx, h, "/dev/sda", 2, 5, nil)).ToNot(HaveOccurred())
		var newHost models.Host
		Expect(db.Take(&newHost, "id = ? and infra_env_id = ?", h.ID.String(), h.InfraEnvID.String()).Error).ToNot(HaveOccurred())
		verifyExistingDiskResult(&newHost, "/dev/sda", 5, 2)
		Expect(m.ResetHostValidation(ctx, *h.ID, h.InfraEnvID, string(models.HostValidationIDSufficientInstallationDiskSpeed), nil)).ToNot(HaveOccurred())
		Expect(db.Take(&newHost, "id = ? and infra_env_id = ?", h.ID.String(), h.InfraEnvID.String()).Error).ToNot(HaveOccurred())
		verifyNonExistentDiskResult(&newHost, "/dev/sda")
	})
	It("One image in error", func() {
		Expect(m.UpdateImageStatus(ctx, h, &models.ContainerImageAvailability{
			Name:   "a.b.c",
			Result: models.ContainerImageAvailabilityResultFailure,
		}, db)).ToNot(HaveOccurred())
		var newHost models.Host
		Expect(db.Take(&newHost, "id = ? and cluster_id = ?", h.ID.String(), h.ClusterID.String()).Error).ToNot(HaveOccurred())
		imageStatuses, err := common.UnmarshalImageStatuses(newHost.ImagesStatus)
		Expect(err).ToNot(HaveOccurred())
		imageStatus, exists := common.GetImageStatus(imageStatuses, "a.b.c")
		Expect(exists).To(BeTrue())
		Expect(imageStatus.Result).To(Equal(models.ContainerImageAvailabilityResultFailure))
		Expect(m.ResetHostValidation(ctx, *h.ID, h.InfraEnvID, string(models.HostValidationIDContainerImagesAvailable), nil)).ToNot(HaveOccurred())
		Expect(db.Take(&newHost, "id = ? and cluster_id = ?", h.ID.String(), h.ClusterID.String()).Error).ToNot(HaveOccurred())
		imageStatuses, err = common.UnmarshalImageStatuses(newHost.ImagesStatus)
		Expect(err).ToNot(HaveOccurred())
		_, exists = common.GetImageStatus(imageStatuses, "a.b.c")
		Expect(exists).To(BeFalse())
	})
	It("Unsupported validation", func() {
		Expect(m.ResetHostValidation(ctx, *h.ID, h.InfraEnvID, string(models.HostValidationIDIgnitionDownloadable), nil)).To(HaveOccurred())
	})
	It("Nonexistant validation", func() {
		Expect(m.ResetHostValidation(ctx, *h.ID, h.InfraEnvID, "abcd", nil)).To(HaveOccurred())
	})
	It("Host not found", func() {
		err := m.ResetHostValidation(ctx, strfmt.UUID(uuid.New().String()), h.InfraEnvID, string(models.HostValidationIDContainerImagesAvailable), nil)
		Expect(err).To(HaveOccurred())
		apiErr, ok := err.(*common.ApiErrorResponse)
		Expect(ok).To(BeTrue())
		Expect(apiErr.StatusCode()).To(BeNumerically("==", http.StatusNotFound))
	})
	It("Unassigned host", func() {
		h = registerUnassignedTestHost(strfmt.UUID(uuid.New().String()))
		err := m.ResetHostValidation(ctx, *h.ID, h.InfraEnvID, string(models.HostValidationIDContainerImagesAvailable), nil)
		Expect(err).To(HaveOccurred())
		apiErr, ok := err.(*common.ApiErrorResponse)
		Expect(ok).To(BeTrue())
		Expect(apiErr.StatusCode()).To(BeNumerically("==", http.StatusInternalServerError))
	})
})

var _ = Describe("Disabled Host Validation", func() {
	const (
		disabledHostValidationEnvironmentName = "DISABLED_HOST_VALIDATIONS"
		twoValidationIDs                      = "validation-1,validation-2"
		malformedValue                        = "validation-1,,"
	)

	AfterEach(func() {
		os.Unsetenv(disabledHostValidationEnvironmentName)
	})
	It("should have values when environment is defined", func() {
		Expect(os.Setenv(disabledHostValidationEnvironmentName, twoValidationIDs)).NotTo(HaveOccurred())
		cfg := Config{}
		Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).ToNot(HaveOccurred())
		Expect(cfg.DisabledHostvalidations.IsDisabled("validation-1")).To(BeTrue())
		Expect(cfg.DisabledHostvalidations.IsDisabled("validation-2")).To(BeTrue())
	})
	It("should error when environment value is malformed", func() {
		Expect(os.Setenv(disabledHostValidationEnvironmentName, malformedValue)).NotTo(HaveOccurred())
		cfg := Config{}
		err := envconfig.Process(common.EnvConfigPrefix, &cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("envconfig.Process: assigning MYAPP_DISABLED_HOST_VALIDATIONS to DisabledHostvalidations: converting 'validation-1,,' to type host.DisabledHostValidations. details: empty host validation ID found in 'validation-1,,'"))
	})

})

var _ = Describe("Get host by Kube key", func() {
	var (
		state            API
		ctrl             *gomock.Controller
		db               *gorm.DB
		key              types.NamespacedName
		kubeKeyNamespace = "test-kube-namespace"
		dbName           string
		mockEvents       *eventsapi.MockHandler
		mockHwValidator  *hardware.MockValidator
		validatorCfg     *hardware.ValidatorCfg
		id               strfmt.UUID
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		validatorCfg = createValidatorCfg()
		mockMetric := metrics.NewMockAPI(ctrl)
		db, dbName = common.PrepareTestDB()
		state = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, mockMetric, defaultConfig, nil, nil, nil)
		id = strfmt.UUID(uuid.New().String())
		key = types.NamespacedName{
			Namespace: kubeKeyNamespace,
			Name:      id.String(),
		}
	})

	It("host not exist", func() {
		h, err := state.GetHostByKubeKey(key)
		Expect(err).Should(HaveOccurred())
		Expect(errors.Is(err, gorm.ErrRecordNotFound)).Should(Equal(true))
		Expect(h).Should(BeNil())
	})

	It("get host by kube key success", func() {
		h1 := common.Host{
			KubeKeyNamespace: kubeKeyNamespace,
			Host:             models.Host{ClusterID: &id, InfraEnvID: id, ID: &id},
		}
		Expect(db.Create(&h1).Error).ShouldNot(HaveOccurred())

		h2, err := state.GetHostByKubeKey(key)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(h2.ID.String()).Should(Equal(h1.ID.String()))
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("Media disconnection", func() {

	var (
		ctx          = context.Background()
		db           *gorm.DB
		api          API
		dbName       string
		config       Config
		mockEventApi *eventsapi.MockHandler
		ctrl         *gomock.Controller
		clusterId    strfmt.UUID
		hostId       strfmt.UUID
		infraEnvId   strfmt.UUID
		host         models.Host
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		config = *defaultConfig
		ctrl = gomock.NewController(GinkgoT())
		mockEventApi = eventsapi.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		api = NewManager(common.GetTestLog(), db, mockEventApi, nil, nil, nil, nil, &config, dummy, nil, nil)
		clusterId = strfmt.UUID(uuid.New().String())
		hostId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusInsufficient)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	const errorMessage string = "Failed - " + statusInfoMediaDisconnected

	It("Media disconnection occurred before installation", func() {
		mockEventApi.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostMediaDisconnectedEventName),
			eventstest.WithHostIdMatcher(host.ID.String()),
			eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
			eventstest.WithClusterIdMatcher(host.ClusterID.String()))).Times(1)
		mockEventApi.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
			eventstest.WithHostIdMatcher(host.ID.String()),
			eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
			eventstest.WithClusterIdMatcher(host.ClusterID.String()))).Times(1)
		Expect(*host.Status).To(BeEquivalentTo(models.HostStatusInsufficient))
		Expect(api.HandleMediaDisconnected(ctx, &host)).ShouldNot(HaveOccurred())
		tx := db.Take(&host, "cluster_id = ? and id = ?", clusterId.String(), hostId.String())
		Expect(tx.Error).ToNot(HaveOccurred())
		Expect(*host.Status).To(BeEquivalentTo(models.HostMediaStatusDisconnected))
		Expect(*host.StatusInfo).To(BeEquivalentTo(statusInfoMediaDisconnected))
		Expect(*host.MediaStatus).To(BeEquivalentTo(models.HostMediaStatusDisconnected))
	})

	It("Media disconnection - wrapping an existing error", func() {
		updates := map[string]interface{}{}
		updates["Status"] = models.HostStatusError
		updates["StatusInfo"] = "error"
		Expect(db.Model(host).Updates(updates).Error).ShouldNot(HaveOccurred())
		tx := db.Take(&host, "cluster_id = ? and id = ?", clusterId.String(), hostId.String())
		Expect(tx.Error).ToNot(HaveOccurred())
		Expect(*host.Status).To(BeEquivalentTo(models.HostStatusError))
		Expect(*host.StatusInfo).To(BeEquivalentTo("error"))
		mockEventApi.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostMediaDisconnectedEventName),
			eventstest.WithHostIdMatcher(host.ID.String()),
			eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
			eventstest.WithClusterIdMatcher(host.ClusterID.String()))).Times(1)
		Expect(api.HandleMediaDisconnected(ctx, &host)).ShouldNot(HaveOccurred())
		tx = db.Take(&host, "cluster_id = ? and id = ?", clusterId.String(), hostId.String())
		Expect(tx.Error).ToNot(HaveOccurred())
		Expect(*host.Status).To(BeEquivalentTo(models.HostStatusError))
		Expect(*host.StatusInfo).To(BeEquivalentTo(errorMessage + ". error"))
		Expect(*host.MediaStatus).To(BeEquivalentTo(models.HostMediaStatusDisconnected))
	})

	It("Media disconnection - repeated errors", func() {
		updates := map[string]interface{}{}
		updates["Status"] = models.HostStatusError
		updates["StatusInfo"] = errorMessage
		updates["MediaStatus"] = models.HostMediaStatusDisconnected
		Expect(db.Model(host).Updates(updates).Error).ShouldNot(HaveOccurred())
		tx := db.Take(&host, "cluster_id = ? and id = ?", clusterId.String(), hostId.String())
		Expect(tx.Error).ToNot(HaveOccurred())
		Expect(*host.MediaStatus).To(BeEquivalentTo(models.HostMediaStatusDisconnected))
		Expect(*host.Status).To(BeEquivalentTo(models.HostStatusError))
		Expect(api.HandleMediaDisconnected(ctx, &host)).ShouldNot(HaveOccurred())
		tx = db.Take(&host, "cluster_id = ? and id = ?", clusterId.String(), hostId.String())
		Expect(tx.Error).ToNot(HaveOccurred())
		Expect(*host.Status).To(BeEquivalentTo(models.HostStatusError))
		Expect(*host.StatusInfo).To(BeEquivalentTo(errorMessage))
		Expect(*host.MediaStatus).To(BeEquivalentTo(models.HostMediaStatusDisconnected))
	})

	It("Media disconnection - reconnection", func() {
		updates := map[string]interface{}{}
		updates["MediaStatus"] = models.HostMediaStatusDisconnected
		Expect(db.Model(host).Updates(updates).Error).ShouldNot(HaveOccurred())
		tx := db.Take(&host, "cluster_id = ? and id = ?", clusterId.String(), hostId.String())
		Expect(tx.Error).ToNot(HaveOccurred())
		Expect(*host.MediaStatus).To(BeEquivalentTo(models.HostMediaStatusDisconnected))
		Expect(api.UpdateMediaConnected(ctx, &host)).ShouldNot(HaveOccurred())
		Expect(*host.MediaStatus).To(BeEquivalentTo(models.HostMediaStatusConnected))
	})
})

var _ = Describe("Installation stages", func() {

	var (
		ctx          = context.Background()
		db           *gorm.DB
		api          API
		dbName       string
		ctrl         *gomock.Controller
		config       Config
		mockEventApi *eventsapi.MockHandler
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		config = *defaultConfig
		dummy := &leader.DummyElector{}
		ctrl = gomock.NewController(GinkgoT())
		mockEventApi = eventsapi.NewMockHandler(ctrl)
		api = NewManager(common.GetTestLog(), db, mockEventApi, nil, nil, nil, nil, &config, dummy, nil, nil)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("IndexOfStage test", func() {
		Expect(api.IndexOfStage(models.HostStageInstalling, MasterStages[:])).To(Equal(1))
		Expect(api.IndexOfStage(models.HostStageWaitingForIgnition, MasterStages[:])).To(Equal(-1))
	})

	It("UpdateInstallProgress test - day1 hosts", func() {

		h := hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), strfmt.UUID(uuid.New().String()), strfmt.UUID(uuid.New().String()), models.HostStatusInstalling)
		h.Role = models.HostRoleMaster
		Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

		By("report first progress", func() {

			newStage := models.HostStageStartingInstallation

			progress := models.HostProgress{
				CurrentStage: newStage,
			}

			mockEventApi.EXPECT().SendHostEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithHostIdMatcher(h.ID.String()),
				eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo)))

			err := api.UpdateInstallProgress(ctx, &h, &progress)
			Expect(err).NotTo(HaveOccurred())

			hFromDB := hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db)
			h = hFromDB.Host
			expectedInstallationPercentage := int64(float64(api.IndexOfStage(newStage, MasterStages[:])+1) / float64(len(MasterStages[:])) * 100)
			Expect(h.Progress.InstallationPercentage).To(Equal(expectedInstallationPercentage))
		})

		By("report another progress", func() {

			newStage := models.HostStageInstalling

			progress := models.HostProgress{
				CurrentStage: newStage,
			}

			mockEventApi.EXPECT().SendHostEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithHostIdMatcher(h.ID.String()),
				eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo)))

			err := api.UpdateInstallProgress(ctx, &h, &progress)
			Expect(err).NotTo(HaveOccurred())

			hFromDB := hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db)
			h = hFromDB.Host
			expectedInstallationPercentage := int64(float64(api.IndexOfStage(newStage, MasterStages[:])+1) / float64(len(MasterStages[:])) * 100)
			Expect(h.Progress.InstallationPercentage).To(Equal(expectedInstallationPercentage))
		})
	})

	It("UpdateInstallProgress test - day2 hosts", func() {

		h := hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), strfmt.UUID(uuid.New().String()), strfmt.UUID(uuid.New().String()), models.HostStatusInstalling)
		hostKindDay2 := models.HostKindAddToExistingClusterHost
		h.Kind = &hostKindDay2
		h.Role = models.HostRoleWorker
		Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

		By("report first progress", func() {

			newStage := models.HostStageStartingInstallation
			progress := models.HostProgress{
				CurrentStage: newStage,
			}

			mockEventApi.EXPECT().SendHostEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithHostIdMatcher(h.ID.String()),
				eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo)))

			err := api.UpdateInstallProgress(ctx, &h, &progress)
			Expect(err).NotTo(HaveOccurred())

			hFromDB := hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db)
			h = hFromDB.Host
			expectedInstallationPercentage := int64(20)
			Expect(h.Progress.InstallationPercentage).To(Equal(expectedInstallationPercentage))
		})

		By("report second progress", func() {

			newStage := models.HostStageInstalling
			progress := models.HostProgress{
				CurrentStage: newStage,
			}

			mockEventApi.EXPECT().SendHostEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithHostIdMatcher(h.ID.String()),
				eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo)))

			err := api.UpdateInstallProgress(ctx, &h, &progress)
			Expect(err).NotTo(HaveOccurred())

			hFromDB := hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db)
			h = hFromDB.Host
			expectedInstallationPercentage := int64(40)
			Expect(h.Progress.InstallationPercentage).To(Equal(expectedInstallationPercentage))
		})

		By("report third progress", func() {

			newStage := models.HostStageWritingImageToDisk
			progress := models.HostProgress{
				CurrentStage: newStage,
			}

			mockEventApi.EXPECT().SendHostEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithHostIdMatcher(h.ID.String()),
				eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo)))

			err := api.UpdateInstallProgress(ctx, &h, &progress)
			Expect(err).NotTo(HaveOccurred())

			hFromDB := hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db)
			h = hFromDB.Host
			expectedInstallationPercentage := int64(60)
			Expect(h.Progress.InstallationPercentage).To(Equal(expectedInstallationPercentage))
		})

		By("report last progress", func() {

			newStage := models.HostStageRebooting
			progress := models.HostProgress{
				CurrentStage: newStage,
			}

			mockEventApi.EXPECT().SendHostEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithHostIdMatcher(h.ID.String()),
				eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo)))

			err := api.UpdateInstallProgress(ctx, &h, &progress)
			Expect(err).NotTo(HaveOccurred())

			hFromDB := hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db)
			h = hFromDB.Host
			expectedInstallationPercentage := int64(100)
			Expect(h.Progress.InstallationPercentage).To(Equal(expectedInstallationPercentage))
			Expect(*h.Status).To(Equal(models.HostStatusAddedToExistingCluster))
		})
	})
})

var _ = Describe("sortHost by hardware", func() {
	var (
		ctrl    *gomock.Controller
		diskID1 = "/dev/disk/by-id/test-disk-1"
		diskID2 = "/dev/disk/by-id/test-disk-2"
		diskID3 = "/dev/disk/by-id/test-disk-3"
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	hwSpecifications := []struct {
		description string
		Cpus        int64
		Ram         int64
		Disks       []*models.Disk
	}{
		{
			description: "minimal master with 3 disks (total of 120 GB)",
			Cpus:        4,
			Ram:         16,
			Disks: []*models.Disk{
				{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID1},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID3},
			},
		},
		{
			description: "minimal master with no disks",
			Cpus:        4,
			Ram:         16,
			Disks:       []*models.Disk{},
		},
		{
			description: "minimal master with 3 disks (total of 80 GB)",
			Cpus:        4,
			Ram:         16,
			Disks: []*models.Disk{
				{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
				{SizeBytes: 20 * conversions.GB, DriveType: "SSD", ID: diskID2},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID3},
			},
		},
		{
			description: "odf master with 3 disks (total of 120 GB)",
			Cpus:        12,
			Ram:         32,
			Disks: []*models.Disk{
				{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID1},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID3},
			},
		},
		{
			description: "sno master with 3 disks (total of 120 GB)",
			Cpus:        8,
			Ram:         32,
			Disks: []*models.Disk{
				{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID1},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID3},
			},
		},
		{
			description: "insufficient for both master and worker",
			Cpus:        2,
			Ram:         4,
			Disks: []*models.Disk{
				{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID1},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID3},
			},
		},
		{
			description: "minimal worker with 3 disks (total of 120 GB)",
			Cpus:        2,
			Ram:         8,
			Disks: []*models.Disk{
				{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID1},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID3},
			},
		},
		{
			description: "odf worker with 3 disks (total of 120 GB)",
			Cpus:        12,
			Ram:         64,
			Disks: []*models.Disk{
				{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID1},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID3},
			},
		},
		{
			description: "odf worker with 1 disk of 40 GB",
			Cpus:        12,
			Ram:         64,
			Disks: []*models.Disk{
				{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID1},
			},
		},
		{
			description: "odf worker with 3 disks (total of 80 GB)",
			Cpus:        12,
			Ram:         64,
			Disks: []*models.Disk{
				{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
				{SizeBytes: 20 * conversions.GB, DriveType: "SSD", ID: diskID2},
				{SizeBytes: 20 * conversions.GB, DriveType: "SSD", ID: diskID3},
			},
		},
	}

	generateHosts := func() []*models.Host {
		hosts := make([]*models.Host, 0)
		for _, spec := range hwSpecifications {
			inventory := &models.Inventory{
				CPU: &models.CPU{
					Count: spec.Cpus,
				},
				Memory: &models.Memory{
					UsableBytes: conversions.GibToBytes(spec.Ram),
				},
				Disks: funk.Map(spec.Disks, func(d *models.Disk) *models.Disk {
					d.InstallationEligibility = models.DiskInstallationEligibility{
						Eligible: true,
					}
					return d
				}).([]*models.Disk),
			}
			b, _ := json.Marshal(inventory)
			id := strfmt.UUID(uuid.New().String())
			hosts = append(hosts, &models.Host{
				ID:                &id,
				RequestedHostname: spec.description,
				Inventory:         string(b),
			})
		}
		return hosts
	}

	It("verify host order", func() {
		sorted, _ := SortHosts(generateHosts())
		expected := []string{
			"insufficient for both master and worker",
			"minimal worker with 3 disks (total of 120 GB)",
			"minimal master with no disks",
			"minimal master with 3 disks (total of 80 GB)",
			"minimal master with 3 disks (total of 120 GB)",
			"sno master with 3 disks (total of 120 GB)",
			"odf master with 3 disks (total of 120 GB)",
			"odf worker with 1 disk of 40 GB",
			"odf worker with 3 disks (total of 80 GB)",
			"odf worker with 3 disks (total of 120 GB)",
		}
		for i, h := range sorted {
			Expect(h.RequestedHostname).To(Equal(expected[i]))
		}
	})
})

var _ = Describe("update node labels", func() {
	var (
		ctx                       = context.Background()
		db                        *gorm.DB
		state                     API
		host                      models.Host
		id, clusterID, infraEnvID strfmt.UUID
		dbName                    string
	)

	BeforeEach(func() {
		dummy := &leader.DummyElector{}
		db, dbName = common.PrepareTestDB()
		state = NewManager(common.GetTestLog(), db, nil, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil, nil)
		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("update node labels by src state", func() {

		nodeLabelsStr := `{"node.ocs.openshift.io/storage":""}`

		success := func(srcState string) {
			host = hostutil.GenerateTestHost(id, infraEnvID, clusterID, srcState)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateNodeLabels(ctx, &host, nodeLabelsStr, nil)).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(id, infraEnvID, db)
			Expect(h.NodeLabels).To(Equal(nodeLabelsStr))
		}

		failure := func(srcState string) {
			host = hostutil.GenerateTestHost(id, infraEnvID, clusterID, srcState)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateNodeLabels(ctx, &host, nodeLabelsStr, nil)).To(HaveOccurred())
			h := hostutil.GetHostFromDB(id, infraEnvID, db)
			Expect(h.NodeLabels).To(Equal(""))
		}

		tests := []struct {
			name     string
			srcState string
			testFunc func(srcState string)
		}{
			{
				name:     "discovering",
				srcState: models.HostStatusDiscovering,
				testFunc: success,
			},
			{
				name:     "known",
				srcState: models.HostStatusKnown,
				testFunc: success,
			},
			{
				name:     "disconnected",
				srcState: models.HostStatusDisconnected,
				testFunc: success,
			},
			{
				name:     "insufficient",
				srcState: models.HostStatusInsufficient,
				testFunc: success,
			},
			{
				name:     "pending-for-input",
				srcState: models.HostStatusPendingForInput,
				testFunc: success,
			},
			{
				name:     "discovering-unbound",
				srcState: models.HostStatusDiscoveringUnbound,
				testFunc: success,
			},
			{
				name:     "known-unbound",
				srcState: models.HostStatusKnownUnbound,
				testFunc: success,
			},
			{
				name:     "disconnected-unbound",
				srcState: models.HostStatusDisconnectedUnbound,
				testFunc: success,
			},
			{
				name:     "insufficient-unbound",
				srcState: models.HostStatusInsufficientUnbound,
				testFunc: success,
			},
			{
				name:     "binding",
				srcState: models.HostStatusBinding,
				testFunc: success,
			},
			{
				name:     "error",
				srcState: models.HostStatusError,
				testFunc: failure,
			},
			{
				name:     "installing",
				srcState: models.HostStatusInstalling,
				testFunc: failure,
			},
			{
				name:     "installed",
				srcState: models.HostStatusInstalled,
				testFunc: failure,
			},
			{
				name:     "installing-in-progress",
				srcState: models.HostStatusInstallingInProgress,
				testFunc: failure,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				t.testFunc(t.srcState)
			})
		}
	})
})

var _ = Describe("GetClusterRegisteredAndApprovedHostsSummary", func() {
	uuidPtr := func(u strfmt.UUID) *strfmt.UUID {
		return &u
	}
	var (
		manager API
		db      *gorm.DB
		dbName  string
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		manager = NewManager(common.GetTestLog(), db, nil, nil, nil, nil, nil, defaultConfig, nil, nil, nil)
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
	tests := []struct {
		name               string
		hosts              []*common.Host
		expectedRegistered int
		expectedApproved   int
	}{
		{
			name: "Empty",
		},
		{
			name: "No known hosts",
			hosts: []*common.Host{
				{
					Host: models.Host{
						Status: swag.String(models.HostStatusInsufficient),
					},
				},
				{
					Host: models.Host{
						Status: swag.String(models.HostStatusDiscovering),
					},
					Approved: true,
				},
			},
		},
		{
			name: "2 known, 1 approved",
			hosts: []*common.Host{
				{
					Host: models.Host{
						Status: swag.String(models.HostStatusKnown),
					},
				},
				{
					Host: models.Host{
						Status: swag.String(models.HostStatusKnown),
					},
					Approved: true,
				},
			},
			expectedApproved:   1,
			expectedRegistered: 2,
		},
		{
			name: "all approved",
			hosts: []*common.Host{
				{
					Host: models.Host{
						Status: swag.String(models.HostStatusKnown),
					},
					Approved: true,
				},
				{
					Host: models.Host{
						Status: swag.String(models.HostStatusKnown),
					},
					Approved: true,
				},
			},
			expectedApproved:   2,
			expectedRegistered: 2,
		},
	}
	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			clusterID := strfmt.UUID(uuid.New().String())
			for _, h := range t.hosts {
				h.ID = uuidPtr(strfmt.UUID(uuid.New().String()))
				h.ClusterID = uuidPtr(clusterID)
				h.InfraEnvID = clusterID
				Expect(db.Create(h).Error).ToNot(HaveOccurred())
			}
			registered, approved, err := manager.GetKnownHostApprovedCounts(clusterID)
			Expect(err).ToNot(HaveOccurred())
			Expect(registered).To(Equal(t.expectedRegistered))
			Expect(approved).To(Equal(t.expectedApproved))
		})
	}
})

var _ = Describe("HostWithCollectedLogsExists", func() {
	uuidPtr := func(u strfmt.UUID) *strfmt.UUID {
		return &u
	}
	var (
		manager API
		db      *gorm.DB
		dbName  string
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		manager = NewManager(common.GetTestLog(), db, nil, nil, nil, nil, nil, defaultConfig, nil, nil, nil)
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
	tests := []struct {
		name              string
		hosts             []*common.Host
		updateAfterCreate bool
		expectedResult    bool
	}{
		{
			name: "Empty",
		},
		{
			name: "Logs not collected",
			hosts: []*common.Host{
				{},
				{},
			},
		},
		{
			name: "Update after create",
			hosts: []*common.Host{
				{},
				{},
			},
			updateAfterCreate: true,
		},
		{
			name: "1 collected",
			hosts: []*common.Host{
				{},
				{
					Host: models.Host{
						LogsCollectedAt: strfmt.DateTime(time.Now()),
					},
				},
			},
			expectedResult: true,
		},
		{
			name: "all collected",
			hosts: []*common.Host{
				{
					Host: models.Host{
						LogsCollectedAt: strfmt.DateTime(time.Now()),
					},
				},
				{
					Host: models.Host{
						LogsCollectedAt: strfmt.DateTime(time.Now()),
					},
				},
			},
			expectedResult: true,
		},
	}
	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			clusterID := strfmt.UUID(uuid.New().String())
			for _, h := range t.hosts {
				h.ID = uuidPtr(strfmt.UUID(uuid.New().String()))
				h.ClusterID = uuidPtr(clusterID)
				h.InfraEnvID = clusterID
				Expect(db.Create(h).Error).ToNot(HaveOccurred())
				if t.updateAfterCreate {
					Expect(db.Model(&models.Host{}).Where("id = ? and infra_env_id = ?", h.ID.String(), h.InfraEnvID.String()).
						Update("logs_collected_at", h.LogsCollectedAt).Error).ToNot(HaveOccurred())
				}
			}
			exists, err := manager.HostWithCollectedLogsExists(clusterID)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(Equal(t.expectedResult))
		})
	}
})

var _ = Describe("GetKnownApprovedHosts", func() {
	uuidPtr := func(u strfmt.UUID) *strfmt.UUID {
		return &u
	}
	var (
		manager API
		db      *gorm.DB
		dbName  string
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		manager = NewManager(common.GetTestLog(), db, nil, nil, nil, nil, nil, defaultConfig, nil, nil, nil)
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
	tests := []struct {
		name        string
		hosts       []*common.Host
		numExpected int
	}{
		{
			name: "Empty",
		},
		{
			name: "No known hosts",
			hosts: []*common.Host{
				{
					Host: models.Host{
						Status: swag.String(models.HostStatusInsufficient),
					},
				},
				{
					Host: models.Host{
						Status: swag.String(models.HostStatusDiscovering),
					},
					Approved: true,
				},
			},
		},
		{
			name: "2 known, 1 approved",
			hosts: []*common.Host{
				{
					Host: models.Host{
						Status: swag.String(models.HostStatusKnown),
					},
				},
				{
					Host: models.Host{
						Status: swag.String(models.HostStatusKnown),
					},
					Approved: true,
				},
			},
			numExpected: 1,
		},
		{
			name: "all approved",
			hosts: []*common.Host{
				{
					Host: models.Host{
						Status: swag.String(models.HostStatusKnown),
					},
					Approved: true,
				},
				{
					Host: models.Host{
						Status: swag.String(models.HostStatusKnown),
					},
					Approved: true,
				},
			},
			numExpected: 2,
		},
	}
	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			clusterID := strfmt.UUID(uuid.New().String())
			for _, h := range t.hosts {
				h.ID = uuidPtr(strfmt.UUID(uuid.New().String()))
				h.ClusterID = uuidPtr(clusterID)
				h.InfraEnvID = clusterID
				Expect(db.Create(h).Error).ToNot(HaveOccurred())
			}
			hosts, err := manager.GetKnownApprovedHosts(clusterID)
			Expect(err).ToNot(HaveOccurred())
			Expect(hosts).To(HaveLen(t.numExpected))
			for _, h := range hosts {
				Expect(swag.StringValue(h.Status)).To(Equal(models.HostStatusKnown))
				Expect(h.Approved).To(BeTrue())
			}
		})
	}
})
