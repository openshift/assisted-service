package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/openshift/assisted-service/pkg/leader"
	"github.com/sirupsen/logrus"
)

var (
	defaultHwInfo                  = "default hw info" // invalid hw info used only for tests
	defaultDisabledHostValidations = DisabledHostValidations{}
	defaultConfig                  = &Config{
		ResetTimeout:            3 * time.Minute,
		EnableAutoReset:         true,
		MonitorBatchSize:        100,
		DisabledHostvalidations: defaultDisabledHostValidations,
	}
	defaultNTPSources = []*models.NtpSource{common.TestNTPSourceSynced}
)

var _ = Describe("update_role", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		host          models.Host
		id, clusterID strfmt.UUID
		dbName        string
	)

	BeforeEach(func() {
		dummy := &leader.DummyElector{}
		db, dbName = common.PrepareTestDB()
		state = NewManager(common.GetTestLog(), db, nil, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil)
		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("update role by src state", func() {
		success := func(srcState string) {
			host = hostutil.GenerateTestHost(id, clusterID, srcState)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, nil)).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(id, clusterID, db)
			Expect(h.Role).To(Equal(models.HostRoleMaster))
		}

		failure := func(srcState string) {
			host = hostutil.GenerateTestHost(id, clusterID, srcState)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, nil)).To(HaveOccurred())
			h := hostutil.GetHostFromDB(id, clusterID, db)
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
				name:     "disabled",
				srcState: models.HostStatusDisabled,
				testFunc: failure,
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

	It("update role with transaction", func() {
		host = hostutil.GenerateTestHost(id, clusterID, models.HostStatusKnown)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		By("rollback transaction", func() {
			tx := db.Begin()
			Expect(tx.Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, tx)).NotTo(HaveOccurred())
			Expect(tx.Rollback().Error).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(id, clusterID, db)
			Expect(h.Role).Should(Equal(models.HostRoleWorker))
		})
		By("commit transaction", func() {
			tx := db.Begin()
			Expect(tx.Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, tx)).NotTo(HaveOccurred())
			Expect(tx.Commit().Error).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(id, clusterID, db)
			Expect(h.Role).Should(Equal(models.HostRoleMaster))
		})
	})

	It("update role master to worker", func() {
		host = hostutil.GenerateTestHost(id, clusterID, models.HostStatusKnown)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, nil)).NotTo(HaveOccurred())
		h := hostutil.GetHostFromDB(id, clusterID, db)
		Expect(h.Role).To(Equal(models.HostRoleMaster))
		Expect(state.UpdateRole(ctx, &host, models.HostRoleWorker, nil)).NotTo(HaveOccurred())
		h = hostutil.GetHostFromDB(id, clusterID, db)
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
					host = hostutil.GenerateTestHostAddedToCluster(id, clusterID, models.HostStatusKnown)
				} else {
					host = hostutil.GenerateTestHost(id, clusterID, models.HostStatusKnown)
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
				h := hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
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
		mockEvents *events.MockHandler
		mockMetric *metrics.MockAPI
		dbName     string
	)

	setDefaultReportHostInstallationMetrics := func(mockMetricApi *metrics.MockAPI) {
		mockMetricApi.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		state = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), mockMetric, defaultConfig, dummy, nil)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(id, clusterId, "")
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
				mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo,
					fmt.Sprintf("Host %s: updated status from \"installing\" to \"installing-in-progress\" (default progress stage)", host.ID.String()),
					gomock.Any())
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
			})

			It("same_value", func() {
				progress.CurrentStage = common.TestDefaultConfig.HostProgressStage
				mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo,
					fmt.Sprintf("Host %s: updated status from \"installing\" to \"installing-in-progress\" (default progress stage)", host.ID.String()),
					gomock.Any())
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
				updatedAt := hostFromDB.StageUpdatedAt.String()

				Expect(state.UpdateInstallProgress(ctx, &hostFromDB.Host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*hostFromDB.ID, host.ClusterID, db)
				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
				Expect(hostFromDB.StageUpdatedAt.String()).Should(Equal(updatedAt))
			})

			It("writing to disk", func() {
				progress.CurrentStage = models.HostStageWritingImageToDisk
				progress.ProgressInfo = "20%"
				mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo,
					fmt.Sprintf("Host %s: updated status from \"installing\" to \"installing-in-progress\" (Writing image to disk)", host.ID.String()),
					gomock.Any())
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
			})

			It("done", func() {
				progress.CurrentStage = models.HostStageDone
				mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo,
					fmt.Sprintf("Host %s: updated status from \"installing\" to \"installed\" (Done)", host.ID.String()),
					gomock.Any())
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)

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
				mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityError,
					fmt.Sprintf("Host %s: updated status from \"installing\" to \"error\" (Failed - reason)", host.ID.String()),
					gomock.Any())
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)

				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusError))
				Expect(*hostFromDB.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", progress.CurrentStage, progress.ProgressInfo)))
			})

			It("progress_failed_empty_reason", func() {
				progress.CurrentStage = models.HostStageFailed
				progress.ProgressInfo = ""
				mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityError,
					fmt.Sprintf("Host %s: updated status from \"installing\" to \"error\" "+
						"(Failed)", host.ID.String()),
					gomock.Any())
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
				Expect(*hostFromDB.Status).Should(Equal(models.HostStatusError))
				Expect(*hostFromDB.StatusInfo).Should(Equal(string(progress.CurrentStage)))
			})

			It("progress_failed_after_a_stage", func() {
				By("Some stage", func() {
					progress.CurrentStage = models.HostStageWritingImageToDisk
					progress.ProgressInfo = "20%"
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo,
						fmt.Sprintf("Host %s: updated status from \"installing\" to \"installing-in-progress\" "+
							"(Writing image to disk)", host.ID.String()),
						gomock.Any())
					Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
					hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
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
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityError,
						fmt.Sprintf("Host %s: updated status from \"installing-in-progress\" to \"error\" "+
							"(Failed - reason)", host.ID.String()),
						gomock.Any())
					Expect(state.UpdateInstallProgress(ctx, &hostFromDB.Host, &newProgress)).ShouldNot(HaveOccurred())
					hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
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
					hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
					Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
					Expect(*hostFromDB.StatusInfo).Should(Equal(string(progress.CurrentStage)))

					Expect(hostFromDB.Progress.CurrentStage).Should(Equal(progress.CurrentStage))
					Expect(hostFromDB.Progress.ProgressInfo).Should(Equal(progress.ProgressInfo))
				}

				By("Some stage", func() {
					progress.CurrentStage = models.HostStageWritingImageToDisk
					progress.ProgressInfo = "20%"
					mockMetric.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo,
						fmt.Sprintf("Host %s: updated status from \"installing\" to \"installing-in-progress\" "+
							"(Writing image to disk)", host.ID.String()),
						gomock.Any())
					Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
					verifyDb()
				})

				By("Lower stage", func() {
					newProgress := models.HostProgress{
						CurrentStage: models.HostStageInstalling,
					}
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo,
						fmt.Sprintf("Host %s: updated status from \"installing\" to \"installing-in-progress\" "+
							"(Writing image to disk)", host.ID.String()),
						gomock.Any())
					Expect(state.UpdateInstallProgress(ctx, &hostFromDB.Host, &newProgress)).Should(HaveOccurred())
					verifyDb()
				})
			})

			It("update_on_installed", func() {
				verifyDb := func() {
					hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)

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
		mockEvents *events.MockHandler
		mockMetric *metrics.MockAPI
		dbName     string
		clusterId  strfmt.UUID
	)

	setDefaultReportHostInstallationMetrics := func(mockMetricApi *metrics.MockAPI) {
		mockMetricApi.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		setDefaultReportHostInstallationMetrics(mockMetric)
		state = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), mockMetric, defaultConfig, dummy, nil)
		id := strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHostByKind(id, clusterId, models.HostStatusInstalling, models.HostKindHost, models.HostRoleMaster)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, host.ID, models.EventSeverityInfo,
			fmt.Sprintf("Host %s: set as bootstrap", host.ID.String()),
			gomock.Any())
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
			cluster := hostutil.GenerateTestCluster(clusterId, "10.0.0.1/24")
			cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

			progress.CurrentStage = models.HostStageWaitingForBootkube
			mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo,
				fmt.Sprintf("Host %s: updated status from \"installing\" to \"installing-in-progress\" (Waiting for bootkube)", host.ID.String()),
				gomock.Any())
			Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
			hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
			Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
			Expect(hostFromDB.Progress.CurrentStage).Should(Equal(models.HostStageWaitingForBootkube))

			progress.CurrentStage = models.HostStageWritingImageToDisk
			progress.ProgressInfo = "20%"
			Expect(state.UpdateInstallProgress(ctx, &hostFromDB.Host, &progress)).ShouldNot(HaveOccurred())
			hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
			Expect(hostFromDB.Progress.CurrentStage).Should(Equal(models.HostStageWritingImageToDisk))
		})
		It("Single node special stage order - not allowed", func() {
			cluster := hostutil.GenerateTestCluster(clusterId, "10.0.0.1/24")
			cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

			progress.CurrentStage = models.HostStageWaitingForBootkube
			mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo,
				fmt.Sprintf("Host %s: updated status from \"installing\" to \"installing-in-progress\" (Waiting for bootkube)", host.ID.String()),
				gomock.Any())
			Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
			hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
			Expect(*hostFromDB.Status).Should(Equal(models.HostStatusInstallingInProgress))
			Expect(hostFromDB.Progress.CurrentStage).Should(Equal(models.HostStageWaitingForBootkube))

			progress.CurrentStage = models.HostStageInstalling
			Expect(state.UpdateInstallProgress(ctx, &hostFromDB.Host, &progress)).Should(HaveOccurred())
		})
		It("multi node update should fail", func() {
			cluster := hostutil.GenerateTestCluster(clusterId, "10.0.0.1/24")
			cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeFull)
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

			progress.CurrentStage = models.HostStageWaitingForBootkube
			mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo,
				fmt.Sprintf("Host %s: updated status from \"installing\" to \"installing-in-progress\" (Waiting for bootkube)", host.ID.String()),
				gomock.Any())
			Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
			hostFromDB = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
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
		eventsHandler events.Handler
		dbName        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		eventsHandler = events.New(db, logrus.New())
		dummy := &leader.DummyElector{}
		state = NewManager(common.GetTestLog(), db, eventsHandler, nil, nil, nil, nil, defaultConfig, dummy, nil)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		h = hostutil.GenerateTestHost(id, clusterId, models.HostStatusDiscovering)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("cancel installation", func() {
		It("cancel installation success", func() {
			h.Status = swag.String(models.HostStatusInstalling)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
			events, err := eventsHandler.GetEvents(h.ClusterID, h.ID)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityInfo))
			eventMessage := fmt.Sprintf("Installation cancelled for host %s", hostutil.GetHostnameForMsg(&h))
			Expect(*cancelEvent.Message).Should(Equal(eventMessage))
		})

		It("cancel failed installation", func() {
			h.Status = swag.String(models.HostStatusError)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
			events, err := eventsHandler.GetEvents(h.ClusterID, h.ID)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityInfo))
			eventMessage := fmt.Sprintf("Installation cancelled for host %s", hostutil.GetHostnameForMsg(&h))
			Expect(*cancelEvent.Message).Should(Equal(eventMessage))
		})

		AfterEach(func() {
			db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
			Expect(*h.Status).Should(Equal(models.HostStatusCancelled))
		})
	})

	Context("invalid cancel installation", func() {
		It("nothing to cancel", func() {
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).Should(HaveOccurred())
			events, err := eventsHandler.GetEvents(h.ClusterID, h.ID)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityError))
		})

		It("cancel disabled host", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			h = hostutil.GenerateTestHost(id, clusterId, models.HostStatusDisabled)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
			Expect(*h.Status).Should(Equal(models.HostStatusDisabled))
			events, err := eventsHandler.GetEvents(h.ClusterID, h.ID)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).Should(Equal(0))
		})
	})
})

var _ = Describe("reset host", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		h             models.Host
		eventsHandler events.Handler
		dbName        string
		config        Config
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		eventsHandler = events.New(db, logrus.New())
		config = *defaultConfig
		dummy := &leader.DummyElector{}
		state = NewManager(common.GetTestLog(), db, eventsHandler, nil, nil, nil, nil, &config, dummy, nil)
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("reset installation", func() {
		It("reset installation success", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			h = hostutil.GenerateTestHost(id, clusterId, models.HostStatusError)
			h.LogsCollectedAt = strfmt.DateTime(time.Now())
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(h.LogsCollectedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))
			Expect(state.ResetHost(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
			Expect(*h.Status).Should(Equal(models.HostStatusResetting))
			events, err := eventsHandler.GetEvents(h.ClusterID, h.ID)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			resetEvent := events[len(events)-1]
			Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityInfo))
			eventMessage := fmt.Sprintf("Installation reset for host %s", hostutil.GetHostnameForMsg(&h))
			Expect(*resetEvent.Message).Should(Equal(eventMessage))
			Expect(h.LogsCollectedAt).Should(Equal(strfmt.DateTime(time.Time{})))
		})

		It("register resetting host", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			h = hostutil.GenerateTestHost(id, clusterId, models.HostStatusResetting)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.RegisterHost(ctx, &h, db)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
			Expect(*h.Status).Should(Equal(models.HostStatusDiscovering))
		})

		It("reset pending user action - passed timeout", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			h = hostutil.GenerateTestHost(id, clusterId, models.HostStatusResetting)
			then := time.Now().Add(-config.ResetTimeout)
			h.StatusUpdatedAt = strfmt.DateTime(then)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.IsRequireUserActionReset(&h)).Should(Equal(true))
			Expect(state.ResetPendingUserAction(ctx, &h, db)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
			Expect(*h.Status).Should(Equal(models.HostStatusResettingPendingUserAction))
			events, err := eventsHandler.GetEvents(h.ClusterID, h.ID)
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
			h = hostutil.GenerateTestHost(id, clusterId, models.HostStatusResetting)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			h.Progress.CurrentStage = models.HostStageRebooting
			Expect(state.IsRequireUserActionReset(&h)).Should(Equal(true))
			Expect(state.ResetPendingUserAction(ctx, &h, db)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
			Expect(*h.Status).Should(Equal(models.HostStatusResettingPendingUserAction))
			events, err := eventsHandler.GetEvents(h.ClusterID, h.ID)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			resetEvent := events[len(events)-1]
			Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityInfo))
			eventMessage := fmt.Sprintf("User action is required in order to complete installation reset for host %s", hostutil.GetHostnameForMsg(&h))
			Expect(*resetEvent.Message).Should(Equal(eventMessage))
		})

		It("reset disabled host", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			h = hostutil.GenerateTestHost(id, clusterId, models.HostStatusDisabled)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.ResetHost(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
			Expect(*h.Status).Should(Equal(models.HostStatusDisabled))
			events, err := eventsHandler.GetEvents(h.ClusterID, h.ID)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).Should(Equal(0))
		})
	})

	Context("invalid_reset_installation", func() {
		It("nothing_to_reset", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			h = hostutil.GenerateTestHost(id, clusterId, models.HostStatusDiscovering)
			reply := state.ResetHost(ctx, &h, "some reason", db)
			Expect(int(reply.StatusCode())).Should(Equal(http.StatusConflict))
			events, err := eventsHandler.GetEvents(h.ClusterID, h.ID)
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
		eventsHandler *events.MockHandler
		dbName        string
		config        Config
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		eventsHandler = events.NewMockHandler(ctrl)
		config = *defaultConfig
		dummy := &leader.DummyElector{}
		state = NewManager(common.GetTestLog(), db, eventsHandler, nil, nil, nil, nil, &config, dummy, nil)
	})

	BeforeEach(func() {
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		h = hostutil.GenerateTestHost(id, clusterId, models.HostStatusDiscovering)
	})

	It("register host success", func() {
		Expect(state.RegisterHost(ctx, &h, db)).ShouldNot(HaveOccurred())
		db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
		Expect(*h.Status).Should(Equal(models.HostStatusDiscovering))
	})

	It("register (soft) deleted host success", func() {
		Expect(state.RegisterHost(ctx, &h, db)).ShouldNot(HaveOccurred())
		db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
		Expect(*h.Status).Should(Equal(models.HostStatusDiscovering))
		Expect(db.Delete(&h).RowsAffected).Should(Equal(int64(1)))
		Expect(db.Unscoped().Find(&h).RowsAffected).Should(Equal(int64(1)))
		Expect(state.RegisterHost(ctx, &h, db)).ShouldNot(HaveOccurred())
		db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
		Expect(*h.Status).Should(Equal(models.HostStatusDiscovering))

	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

})

// alternativeInventory returns an inventory that is arbitrarily slightly different than
// the default inventory. Useful in some tests.
func alternativeInventory() models.Inventory {
	// Create an inventory that is slightly arbitrarily different than the default one
	// (in this case timestamp is set to some magic value other than the default 0)
	// so we can check if UpdateInventory actually occurred
	var newInventory models.Inventory
	magicTimestamp := int64(0xcafecafe)
	err := json.Unmarshal([]byte(common.GenerateTestDefaultInventory()), &newInventory)
	Expect(err).To(BeNil())
	newInventory.Timestamp = magicTimestamp
	return newInventory
}

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
		Timestamp:    1601835002,
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
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

var _ = Describe("UpdateInventory", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockValidator     *hardware.MockValidator
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		ctrl = gomock.NewController(GinkgoT())
		mockValidator = hardware.NewMockValidator(ctrl)
		hapi = NewManager(common.GetTestLog(), db, nil, mockValidator,
			nil, createValidatorCfg(), nil, defaultConfig, dummy, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		cluster := hostutil.GenerateTestCluster(clusterId, "10.0.0.1/24")
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
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
				host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusDiscovering)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(test.inventory.Disks)
				inventoryStr, err := hostutil.MarshalInventory(&test.inventory)
				Expect(err).ToNot(HaveOccurred())
				Expect(hapi.(*Manager).UpdateInventory(ctx, &host, inventoryStr)).ToNot(HaveOccurred())
				h := hostutil.GetHostFromDB(hostId, clusterId, db)
				Expect(h.Inventory).To(Not(BeEmpty()))
				inventory, err := hostutil.UnmarshalInventory(h.Inventory)
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
				mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(append(test.agentReasons, test.serviceReasons...), nil)

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

				Expect(hapi.(*Manager).populateDisksEligibility(ctx, &testInventory, nil, nil)).ShouldNot(HaveOccurred())
				Expect(testInventory.Disks).Should(Equal(expectedDisks))
			})
		}
	})

	Context("Test update default installation disk", func() {
		const (
			diskName = "FirstDisk"
			diskId   = "/dev/disk/by-id/FirstDisk"
			diskPath = "/dev/FirstDisk"
		)

		BeforeEach(func() {
			host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusDiscovering)
			host.Inventory = common.GenerateTestDefaultInventory()
			host.InstallationDiskPath = ""
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

		})

		It("Make sure UpdateInventory updates the db", func() {
			mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(
				[]*models.Disk{{ID: diskId, Name: diskName}},
			)

			Expect(hapi.UpdateInventory(ctx, &host, host.Inventory)).ToNot(HaveOccurred())

			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(h.InstallationDiskPath).To(Equal(diskPath))
			Expect(h.InstallationDiskID).To(Equal(diskId))

			// Now make sure it gets removed if the disk is no longer in the inventory
			mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
			mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(
				[]*models.Disk{},
			)
			Expect(hapi.UpdateInventory(ctx, &host, host.Inventory)).ToNot(HaveOccurred())

			h = hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(h.InstallationDiskPath).To(Equal(""))
			Expect(h.InstallationDiskID).To(Equal(""))
		})

		It("Upgrade installation_disk_id after getting new inventory", func() {
			mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(
				[]*models.Disk{{Name: diskName}},
			)
			Expect(hapi.UpdateInventory(ctx, &host, host.Inventory)).ToNot(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(h.InstallationDiskPath).To(Equal(diskPath))
			Expect(h.InstallationDiskID).To(Equal(diskPath))

			mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(
				[]*models.Disk{{ID: diskId, Name: diskName}},
			)
			mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
			Expect(hapi.UpdateInventory(ctx, &host, host.Inventory)).ToNot(HaveOccurred())
			h = hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(h.InstallationDiskPath).To(Equal(diskPath))
			Expect(h.InstallationDiskID).To(Equal(diskId))
		})
	})

	Context("Inventory changes", func() {
		BeforeEach(func() {
			host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusDiscovering)
			host.Inventory = common.GenerateTestDefaultInventory()
			host.InstallationDiskPath = ""
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		})
		It("Invariant changes to inventory", func() {
			mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(
				[]*models.Disk{},
			).AnyTimes()
			Expect(hapi.UpdateInventory(ctx, &host, host.Inventory)).ToNot(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			updatedAt := h.UpdatedAt
			var inventory models.Inventory
			Expect(json.Unmarshal([]byte(h.Inventory), &inventory)).ToNot(HaveOccurred())
			inventory.Timestamp = 555
			inventory.Disks[0].Smart = "kuku"
			b, err := json.Marshal(&inventory)
			Expect(err).ToNot(HaveOccurred())
			time.Sleep(time.Second)
			Expect(hapi.UpdateInventory(ctx, &host, string(b))).ToNot(HaveOccurred())
			h = hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(updatedAt).To(Equal(h.UpdatedAt))
			Expect(h.Inventory).To(Equal(string(b)))
		})

		It("Variant changes to inventory", func() {
			mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(
				[]*models.Disk{},
			).AnyTimes()
			Expect(hapi.UpdateInventory(ctx, &host, host.Inventory)).ToNot(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			updatedAt := h.UpdatedAt
			var inventory models.Inventory
			Expect(json.Unmarshal([]byte(h.Inventory), &inventory)).ToNot(HaveOccurred())
			inventory.Interfaces = append(inventory.Interfaces, &models.Interface{
				HasCarrier: false,
				Mtu:        1500,
				Name:       "abcd",
			})
			b, err := json.Marshal(&inventory)
			Expect(err).ToNot(HaveOccurred())
			time.Sleep(time.Second)
			Expect(hapi.UpdateInventory(ctx, &host, string(b))).ToNot(HaveOccurred())
			h = hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(updatedAt).ToNot(Equal(h.UpdatedAt))
			Expect(h.Inventory).To(Equal(string(b)))
		})
	})

	Context("enable host", func() {
		var newInventoryBytes []byte

		BeforeEach(func() {
			// Create an inventory that is slightly arbitrarily different than the default one
			// so we can make sure an update actually occurred.
			newInventory := alternativeInventory()
			newInventoryTmp, err := json.Marshal(&newInventory)
			Expect(err).To(BeNil())
			newInventoryBytes = newInventoryTmp

			mockValidator.EXPECT().DiskIsEligible(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, disk *models.Disk, _ *common.Cluster, _ *models.Host) ([]string, error) {
				return disk.InstallationEligibility.NotEligibleReasons, nil
			})
			mockValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return(nil).AnyTimes()
		})

		success := func(err error) {
			Expect(err).To(BeNil())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(h.Inventory).To(Equal(string(newInventoryBytes)))
		}

		failure := func(err error) {
			Expect(err).To(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(h.Inventory).To(Equal(common.GenerateTestDefaultInventory()))
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
				name:       models.HostStatusDisabled,
				srcState:   models.HostStatusDisabled,
				validation: failure,
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
				validation: success,
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
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				host = hostutil.GenerateTestHost(hostId, clusterId, t.srcState)
				host.Inventory = common.GenerateTestDefaultInventory()
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				t.validation(hapi.UpdateInventory(ctx, &host, string(newInventoryBytes)))
			})
		}
	})
})

var _ = Describe("Update hostname", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		dummy := &leader.DummyElector{}
		hapi = NewManager(common.GetTestLog(), db, nil, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("set hostname", func() {
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(h.RequestedHostname).To(Equal("my-hostname"))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
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
				name:       models.HostStatusDisabled,
				srcState:   models.HostStatusDisabled,
				validation: failure,
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
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				host = hostutil.GenerateTestHost(hostId, clusterId, t.srcState)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				t.validation(hapi.UpdateHostname(ctx, &host, "my-hostname", db))
			})
		}
	})
})

var _ = Describe("Update disk installation path", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		ctrl              *gomock.Controller
		mockValidator     *hardware.MockValidator
		dbName            string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		leader := &leader.DummyElector{}
		mockValidator = hardware.NewMockValidator(ctrl)
		logger := common.GetTestLog()
		hapi = NewManager(logger, db, nil, mockValidator, nil, createValidatorCfg(), nil, defaultConfig, leader, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	success := func(reply error) {
		Expect(reply).To(BeNil())
		h := hostutil.GetHostFromDB(hostId, clusterId, db)
		Expect(h.InstallationDiskID).To(Equal(common.TestDiskId))
		Expect(h.InstallationDiskPath).To(Equal(common.TestDiskPath))
	}

	failure := func(reply error) {
		Expect(reply).To(HaveOccurred())
		h := hostutil.GetHostFromDB(hostId, clusterId, db)
		Expect(h.InstallationDiskID).To(Equal(""))
		Expect(h.InstallationDiskPath).To(Equal(""))
	}

	Context("validate disk installation path", func() {
		It("illegal disk installation path", func() {
			host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusKnown)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return([]*models.Disk{common.TestDefaultConfig.Disks}, nil)
			failure(hapi.UpdateInstallationDisk(ctx, db, &host, "/no/such/disk"))
		})
		//happy flow is validated implicitly in the next test context
	})

	Context("validate get host valid disks error", func() {
		It("get host valid disks returns an error", func() {
			host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusKnown)
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
				name:       models.HostStatusDisabled,
				srcState:   models.HostStatusDisabled,
				validation: false,
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

				host = hostutil.GenerateTestHost(hostId, clusterId, t.srcState)
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
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEvents        *events.MockHandler
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		hapi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())

		host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusResetting)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		h := hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
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
				mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, &hostId, models.EventSeverityInfo,
					fmt.Sprintf("Host %s: set as bootstrap", host.ID.String()),
					gomock.Any())

			}
			Expect(hapi.SetBootstrap(ctx, &host, t.IsBootstrap, db)).ShouldNot(HaveOccurred())

			h := hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
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
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEvents        *events.MockHandler
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		hapi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())

		host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusResetting)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		h := hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
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

			h := hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)

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
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEvents        *events.MockHandler
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		hapi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
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
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			// Setup
			if t.day2 {
				host = hostutil.GenerateTestHostAddedToCluster(hostId, clusterId, t.status)
			} else {
				host = hostutil.GenerateTestHost(hostId, clusterId, t.status)
			}

			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

			h := hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
			Expect(h.MachineConfigPoolName).Should(BeEmpty())

			// Test
			err := hapi.UpdateMachineConfigPoolName(ctx, db, &host, t.name)
			h = hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)

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

var _ = Describe("update logs_info", func() {
	var (
		ctx               = context.Background()
		ctrl              *gomock.Controller
		db                *gorm.DB
		hapi              API
		host              models.Host
		hostId, clusterId strfmt.UUID
		dbName            string
	)

	BeforeEach(func() {
		dummy := &leader.DummyElector{}
		db, dbName = common.PrepareTestDB()
		mockOperators := operators.NewMockAPI(ctrl)
		hapi = NewManager(common.GetTestLog(), db, nil, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, mockOperators)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusInstallingInProgress)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	validateLogsStartedAt := func(h *models.Host) {
		Expect(h.LogsStartedAt).NotTo(Equal(strfmt.DateTime(time.Time{})))
	}

	// validateLogsCollectedAt := func(h *models.Host) {
	// 	Expect(h.LogsCollectedAt).NotTo(Equal(strfmt.DateTime(time.Time{})))
	// }

	validateCollectedAtNotUpdated := func(h *models.Host) {
		Expect(h.LogsCollectedAt).To(Equal(strfmt.DateTime(time.Time{})))
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
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(h.LogsInfo).To(Equal(t.logsInfo))
			t.validateTimestamp(&h.Host)
		})
	}
})

var _ = Describe("UpdateImageStatus", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEvents        *events.MockHandler
		mockMetric        *metrics.MockAPI
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		hapi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), mockMetric, defaultConfig, dummy, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())

		host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusResetting)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		h := hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)
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
		name                  string
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
					eventMsg += fmt.Sprintf(" time: %f seconds; size: %f bytes; download rate: %f MBps",
						expectedImage.Time, expectedImage.SizeBytes, expectedImage.DownloadRate)
				}

				mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, &hostId, models.EventSeverityInfo, eventMsg, gomock.Any()).Times(1)
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
			h := hostutil.GetHostFromDB(*host.ID, host.ClusterID, db)

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
		ctx               = context.Background()
		hostApi           API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEvents        *events.MockHandler
		hostId, clusterId strfmt.UUID
		dbName            string
		host              common.Host
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		hostApi = NewManager(common.GetTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, dummy, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())

		host = common.Host{
			Host:             hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusKnown),
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
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
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
		hapi            API
		db              *gorm.DB
		ctrl            *gomock.Controller
		mockHwValidator *hardware.MockValidator
		dbName          string
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockHwValidator = hardware.NewMockValidator(ctrl)
		mockHwValidator.EXPECT().ListEligibleDisks(gomock.Any()).AnyTimes()
		mockHwValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(nil, nil).AnyTimes()
		mockOperators := operators.NewMockAPI(ctrl)
		db, dbName = common.PrepareTestDB()
		clusterId = strfmt.UUID(uuid.New().String())
		dummy := &leader.DummyElector{}
		hapi = NewManager(
			common.GetTestLog(),
			db,
			nil,
			mockHwValidator,
			nil,
			createValidatorCfg(),
			nil,
			defaultConfig,
			dummy,
			mockOperators,
		)
		Expect(db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterId}}).Error).ShouldNot(HaveOccurred())
		mockOperators.EXPECT().ValidateHost(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.HostValidationIDOcsRequirementsSatisfied)},
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
		db.Close()
		ctrl.Finish()
	})

	Context("single host role selection", func() {
		tests := []struct {
			name         string
			srcRole      models.HostRole
			inventory    string
			expectError  bool
			expectedRole models.HostRole
		}{
			{
				name:         "role already set to worker",
				srcRole:      models.HostRoleWorker,
				inventory:    hostutil.GenerateMasterInventory(),
				expectError:  false,
				expectedRole: models.HostRoleWorker,
			}, {
				name:         "role already set to master",
				srcRole:      models.HostRoleMaster,
				inventory:    hostutil.GenerateMasterInventory(),
				expectError:  false,
				expectedRole: models.HostRoleMaster,
			}, {
				name:         "no inventory",
				srcRole:      models.HostRoleAutoAssign,
				inventory:    "",
				expectError:  true,
				expectedRole: models.HostRoleAutoAssign,
			}, {
				name:         "auto-assign master",
				srcRole:      models.HostRoleAutoAssign,
				inventory:    hostutil.GenerateMasterInventory(),
				expectError:  false,
				expectedRole: models.HostRoleMaster,
			}, {
				name:         "auto-assign worker",
				srcRole:      models.HostRoleAutoAssign,
				inventory:    workerInventory(),
				expectError:  false,
				expectedRole: models.HostRoleWorker,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				h := hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), clusterId, models.HostStatusKnown)
				h.Inventory = t.inventory
				h.Role = t.srcRole
				Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
				err := hapi.AutoAssignRole(ctx, &h, db)
				if t.expectError {
					Expect(err).Should(HaveOccurred())
				} else {
					Expect(err).ShouldNot(HaveOccurred())
				}
				Expect(hostutil.GetHostFromDB(*h.ID, clusterId, db).Role).Should(Equal(t.expectedRole))
			})
		}
	})

	It("cluster already have enough master nodes", func() {
		for i := 0; i < common.MinMasterHostsNeededForInstallation; i++ {
			h := hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), clusterId, models.HostStatusKnown)
			h.Inventory = hostutil.GenerateMasterInventory()
			h.Role = models.HostRoleAutoAssign
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(hapi.AutoAssignRole(ctx, &h, db)).ShouldNot(HaveOccurred())
			Expect(hostutil.GetHostFromDB(*h.ID, clusterId, db).Role).Should(Equal(models.HostRoleMaster))
		}

		h := hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), clusterId, models.HostStatusKnown)
		h.Inventory = hostutil.GenerateMasterInventory()
		h.Role = models.HostRoleAutoAssign
		Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
		Expect(hapi.AutoAssignRole(ctx, &h, db)).ShouldNot(HaveOccurred())
		Expect(hostutil.GetHostFromDB(*h.ID, clusterId, db).Role).Should(Equal(models.HostRoleWorker))
	})
})

var _ = Describe("IsValidMasterCandidate", func() {
	var (
		clusterId strfmt.UUID
		hapi      API
		db        *gorm.DB
		dbName    string
		ctrl      *gomock.Controller
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterId = strfmt.UUID(uuid.New().String())
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
		)
		Expect(db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterId}}).Error).ShouldNot(HaveOccurred())
		mockOperators.EXPECT().ValidateHost(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.HostValidationIDOcsRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied)},
		}, nil)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		db.Close()
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
				h := hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), clusterId, t.srcState)
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
		mockEvents      *events.MockHandler
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

	registerTestHostWithValidations := func(clusterID strfmt.UUID) *models.Host {

		hostID := strfmt.UUID(uuid.New().String())
		h := hostutil.GenerateTestHost(hostID, clusterID, models.HostStatusInsufficient)

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
		mockEvents = events.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		validatorCfg = createValidatorCfg()
		m = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, mockMetric, defaultConfig, nil, nil)
		h = registerTestHostWithValidations(strfmt.UUID(uuid.New().String()))
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

		mockEvents.EXPECT().AddEvent(ctx, h.ClusterID, h.ID, models.EventSeverityInfo, gomock.Any(), gomock.Any())

		vc := generateValidationCtx()
		newValidationRes := generateTestValidationResult(ValidationSuccess)
		var currentValidationRes ValidationsStatus
		err := json.Unmarshal([]byte(h.ValidationsInfo), &currentValidationRes)
		Expect(err).ToNot(HaveOccurred())
		m.reportValidationStatusChanged(ctx, vc, h, newValidationRes, currentValidationRes)

		mockMetric.EXPECT().HostValidationChanged(openshiftVersion, emailDomain, models.HostValidationIDHasMinCPUCores)
		mockEvents.EXPECT().AddEvent(ctx, h.ClusterID, h.ID, models.EventSeverityWarning, gomock.Any(), gomock.Any())

		currentValidationRes = newValidationRes
		newValidationRes = generateTestValidationResult(ValidationFailure)
		m.reportValidationStatusChanged(ctx, vc, h, newValidationRes, currentValidationRes)
	})
})

var _ = Describe("SetDiskSpeed", func() {
	var (
		ctrl            *gomock.Controller
		ctx             = context.Background()
		db              *gorm.DB
		dbName          string
		mockEvents      *events.MockHandler
		mockHwValidator *hardware.MockValidator
		validatorCfg    *hardware.ValidatorCfg
		m               *Manager
		h               *models.Host
	)

	registerTestHost := func(clusterID strfmt.UUID) *models.Host {

		hostID := strfmt.UUID(uuid.New().String())
		h := hostutil.GenerateTestHost(hostID, clusterID, models.HostStatusInsufficient)

		h.Inventory = hostutil.GenerateMasterInventory()

		Expect(m.RegisterHost(ctx, &h, db)).ToNot(HaveOccurred())

		return &h
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockEvents = events.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		validatorCfg = createValidatorCfg()
		m = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, nil, defaultConfig, nil, nil)
		h = registerTestHost(strfmt.UUID(uuid.New().String()))
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
		mockEvents      *events.MockHandler
		mockHwValidator *hardware.MockValidator
		validatorCfg    *hardware.ValidatorCfg
		m               *Manager
		h               *models.Host
	)

	registerTestHost := func(clusterID strfmt.UUID) *models.Host {

		hostID := strfmt.UUID(uuid.New().String())
		h := hostutil.GenerateTestHost(hostID, clusterID, models.HostStatusInsufficient)

		h.Inventory = hostutil.GenerateMasterInventory()
		h.InstallationDiskID = "/dev/sda"

		Expect(m.RegisterHost(ctx, &h, db)).ToNot(HaveOccurred())

		return &h
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockEvents = events.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		validatorCfg = createValidatorCfg()
		mockMetric := metrics.NewMockAPI(ctrl)
		m = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, mockMetric, defaultConfig, nil, nil)
		h = registerTestHost(strfmt.UUID(uuid.New().String()))
		mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("/dev/sda").AnyTimes()
		mockMetric.EXPECT().ImagePullStatus(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
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
		Expect(db.Take(&newHost, "id = ? and cluster_id = ?", h.ID.String(), h.ClusterID.String()).Error).ToNot(HaveOccurred())
		verifyExistingDiskResult(&newHost, "/dev/sda", 5, 2)
		Expect(m.ResetHostValidation(ctx, *h.ID, h.ClusterID, string(models.HostValidationIDSufficientInstallationDiskSpeed), nil)).ToNot(HaveOccurred())
		Expect(db.Take(&newHost, "id = ? and cluster_id = ?", h.ID.String(), h.ClusterID.String()).Error).ToNot(HaveOccurred())
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
		Expect(m.ResetHostValidation(ctx, *h.ID, h.ClusterID, string(models.HostValidationIDContainerImagesAvailable), nil)).ToNot(HaveOccurred())
		Expect(db.Take(&newHost, "id = ? and cluster_id = ?", h.ID.String(), h.ClusterID.String()).Error).ToNot(HaveOccurred())
		imageStatuses, err = common.UnmarshalImageStatuses(newHost.ImagesStatus)
		Expect(err).ToNot(HaveOccurred())
		_, exists = common.GetImageStatus(imageStatuses, "a.b.c")
		Expect(exists).To(BeFalse())
	})
	It("Unsupported validation", func() {
		Expect(m.ResetHostValidation(ctx, *h.ID, h.ClusterID, string(models.HostValidationIDAPIVipConnected), nil)).To(HaveOccurred())
	})
	It("Nonexistant validation", func() {
		Expect(m.ResetHostValidation(ctx, *h.ID, h.ClusterID, "abcd", nil)).To(HaveOccurred())
	})
	It("Host not found", func() {
		err := m.ResetHostValidation(ctx, strfmt.UUID(uuid.New().String()), h.ClusterID, string(models.HostValidationIDContainerImagesAvailable), nil)
		Expect(err).To(HaveOccurred())
		apiErr, ok := err.(*common.ApiErrorResponse)
		Expect(ok).To(BeTrue())
		Expect(apiErr.StatusCode()).To(BeNumerically("==", http.StatusNotFound))
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
		fmt.Println("\n" + err.Error())
		Expect(err.Error()).To(Equal("envconfig.Process: assigning MYAPP_DISABLED_HOST_VALIDATIONS to DisabledHostvalidations: converting 'validation-1,,' to type host.DisabledHostValidations. details: empty host validation ID found in 'validation-1,,'"))
	})

})
