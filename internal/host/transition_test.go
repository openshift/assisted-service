package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/hostutil"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/models"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func createValidatorCfg() *hardware.ValidatorCfg {
	return &hardware.ValidatorCfg{
		MinCPUCores:                   2,
		MinCPUCoresWorker:             2,
		MinCPUCoresMaster:             4,
		MinDiskSizeGb:                 120,
		MinRamGib:                     8,
		MinRamGibWorker:               8,
		MinRamGibMaster:               16,
		MaximumAllowedTimeDiffMinutes: 4,
	}
}

var _ = Describe("RegisterHost", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEvents        *events.MockHandler
		hostId, clusterId strfmt.UUID
		dbName            = "register_host"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db = common.PrepareTestDB(dbName, &events.Event{})
		mockEvents = events.NewMockHandler(ctrl)
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	It("register_new", func() {
		Expect(hapi.RegisterHost(ctx, &models.Host{ID: &hostId, ClusterID: clusterId, DiscoveryAgentVersion: "v1.0.1"})).ShouldNot(HaveOccurred())
		h := getHost(hostId, clusterId, db)
		Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusDiscovering))
		Expect(h.DiscoveryAgentVersion).To(Equal("v1.0.1"))
	})

	Context("register during installation", func() {
		tests := []struct {
			name                  string
			progressStage         models.HostStage
			srcState              string
			dstState              string
			errorCode             int32
			expectedEventInfo     string
			expectedEventStatus   string
			expectedNilStatusInfo bool
		}{
			{
				name:                "discovering",
				srcState:            models.HostStatusInstalling,
				dstState:            models.HostStatusError,
				expectedEventInfo:   "Host %s: updated status from \"installing\" to \"error\" (The host unexpectedly restarted during the installation)",
				expectedEventStatus: models.EventSeverityError,
			},
			{
				name:                "insufficient",
				srcState:            models.HostStatusInstallingInProgress,
				dstState:            models.HostStatusError,
				expectedEventInfo:   "Host %s: updated status from \"installing-in-progress\" to \"error\" (The host unexpectedly restarted during the installation)",
				expectedEventStatus: models.EventSeverityError,
			},
			{
				name:                  "pending-user-action",
				progressStage:         models.HostStageRebooting,
				srcState:              models.HostStatusInstallingPendingUserAction,
				dstState:              models.HostStatusInstallingPendingUserAction,
				errorCode:             http.StatusForbidden,
				expectedEventInfo:     "",
				expectedNilStatusInfo: true,
			},
		}
		for i := range tests {
			t := tests[i]

			It(t.name, func() {
				Expect(db.Create(&models.Host{
					ID:        &hostId,
					ClusterID: clusterId,
					Role:      models.HostRoleMaster,
					Inventory: defaultHwInfo,
					Status:    swag.String(t.srcState),
					Progress: &models.HostProgressInfo{
						CurrentStage: t.progressStage,
					},
				}).Error).ShouldNot(HaveOccurred())

				if t.expectedEventInfo != "" && t.expectedEventStatus != "" {
					mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, &hostId, t.expectedEventStatus, fmt.Sprintf(t.expectedEventInfo, hostId.String()), gomock.Any())
				}

				err := hapi.RegisterHost(ctx, &models.Host{
					ID:        &hostId,
					ClusterID: clusterId,
					Status:    swag.String(t.srcState),
				})

				if t.errorCode == 0 {
					Expect(err).ShouldNot(HaveOccurred())
				} else {
					Expect(err).Should(HaveOccurred())
					serr, ok := err.(*common.ApiErrorResponse)
					Expect(ok).Should(Equal(true))
					Expect(serr.StatusCode()).Should(Equal(t.errorCode))
				}
				h := getHost(hostId, clusterId, db)
				Expect(swag.StringValue(h.Status)).Should(Equal(t.dstState))
				Expect(h.Role).Should(Equal(models.HostRoleMaster))
				Expect(h.Inventory).Should(Equal(defaultHwInfo))
				if t.expectedNilStatusInfo {
					Expect(h.StatusInfo).Should(BeNil())
				} else {
					Expect(h.StatusInfo).ShouldNot(BeNil())
				}
			})
		}
	})

	Context("host already exists register success", func() {
		discoveryAgentVersion := "v2.0.5"
		tests := []struct {
			name     string
			srcState string
		}{
			{
				name:     "discovering",
				srcState: models.HostStatusDiscovering,
			},
			{
				name:     "insufficient",
				srcState: models.HostStatusInsufficient,
			},
			{
				name:     "disconnected",
				srcState: models.HostStatusDisconnected,
			},
			{
				name:     "known",
				srcState: models.HostStatusKnown,
			},
		}

		AfterEach(func() {
			h := getHost(hostId, clusterId, db)
			Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusDiscovering))
			Expect(h.Role).Should(Equal(models.HostRoleMaster))
			Expect(h.Inventory).Should(Equal(""))
			Expect(h.DiscoveryAgentVersion).To(Equal(discoveryAgentVersion))
		})

		for i := range tests {
			t := tests[i]

			It(t.name, func() {
				Expect(db.Create(&models.Host{
					ID:        &hostId,
					ClusterID: clusterId,
					Role:      models.HostRoleMaster,
					Inventory: defaultHwInfo,
					Status:    swag.String(t.srcState),
				}).Error).ShouldNot(HaveOccurred())
				if t.srcState != models.HostStatusDiscovering {
					mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, &hostId, models.EventSeverityInfo,
						fmt.Sprintf("Host %s: updated status from \"%s\" to \"discovering\" (%s)",
							hostId.String(), t.srcState, statusInfoDiscovering),
						gomock.Any())
				}

				Expect(hapi.RegisterHost(ctx, &models.Host{
					ID:                    &hostId,
					ClusterID:             clusterId,
					Status:                swag.String(t.srcState),
					DiscoveryAgentVersion: discoveryAgentVersion,
				})).ShouldNot(HaveOccurred())
			})
		}
	})

	Context("host already exist registration fail", func() {
		tests := []struct {
			name        string
			srcState    string
			targetState string
		}{
			{
				name:     "disabled",
				srcState: models.HostStatusDisabled,
			},
			{
				name:     "error",
				srcState: models.HostStatusError,
			},
			{
				name:     "installed",
				srcState: models.HostStatusInstalled,
			},
		}

		for i := range tests {
			t := tests[i]

			It(t.name, func() {
				Expect(db.Create(&models.Host{
					ID:        &hostId,
					ClusterID: clusterId,
					Role:      models.HostRoleMaster,
					Inventory: defaultHwInfo,
					Status:    swag.String(t.srcState),
				}).Error).ShouldNot(HaveOccurred())

				Expect(hapi.RegisterHost(ctx, &models.Host{
					ID:        &hostId,
					ClusterID: clusterId,
					Status:    swag.String(t.srcState),
				})).Should(HaveOccurred())

				h := getHost(hostId, clusterId, db)
				Expect(swag.StringValue(h.Status)).Should(Equal(t.srcState))
				Expect(h.Role).Should(Equal(models.HostRoleMaster))
				Expect(h.Inventory).Should(Equal(defaultHwInfo))
			})
		}
	})

	Context("register after reboot", func() {
		tests := []struct {
			srcState           string
			progress           models.HostProgressInfo
			dstState           string
			eventSeverity      string
			eventMessage       string
			origRole           models.HostRole
			expectedRole       models.HostRole
			expectedStatusInfo string
			expectedInventory  string
			hostKind           string
		}{
			{
				srcState: models.HostStatusInstallingInProgress,
				progress: models.HostProgressInfo{
					CurrentStage: models.HostStageRebooting,
				},
				dstState:      models.HostStatusInstallingPendingUserAction,
				eventSeverity: models.EventSeverityWarning,
				eventMessage: "Host %s: updated status from \"installing-in-progress\" to \"installing-pending-user-action\" " +
					"(Expected the host to boot from disk, but it booted the installation image - please reboot and fix boot " +
					"order to boot from disk /dev/test-disk (test-serial))",
				expectedStatusInfo: "Expected the host to boot from disk, but it booted the installation image - " +
					"please reboot and fix boot order to boot from disk /dev/test-disk (test-serial)",
				expectedRole:      models.HostRoleMaster,
				expectedInventory: defaultInventory(),
				hostKind:          models.HostKindHost,
				origRole:          models.HostRoleMaster,
			},
			{
				srcState: models.HostStatusResetting,
				progress: models.HostProgressInfo{
					CurrentStage: models.HostStageRebooting,
				},
				dstState:          models.HostStatusResetting,
				expectedRole:      models.HostRoleMaster,
				expectedInventory: defaultInventory(),
				hostKind:          models.HostKindHost,
				origRole:          models.HostRoleMaster,
			},
			{
				srcState: models.HostStatusResettingPendingUserAction,
				progress: models.HostProgressInfo{
					CurrentStage: models.HostStageRebooting,
				},
				dstState:      models.HostStatusDiscovering,
				eventSeverity: models.EventSeverityInfo,
				eventMessage: "Host %s: updated status from \"resetting-pending-user-action\" to \"discovering\" " +
					"(Waiting for host to send hardware details)",
				expectedStatusInfo: statusInfoDiscovering,
				expectedRole:       models.HostRoleMaster,
				hostKind:           models.HostKindHost,
				origRole:           models.HostRoleMaster,
			},
			{
				srcState: models.HostStatusAddedToExistingCluster,
				progress: models.HostProgressInfo{
					CurrentStage: models.HostStageRebooting,
				},
				dstState:      models.HostStatusInstallingPendingUserAction,
				eventSeverity: models.EventSeverityWarning,
				eventMessage: "Host %s: updated status from \"added-to-existing-cluster\" to \"installing-pending-user-action\" " +
					"(Expected the host to boot from disk, but it booted the installation image - please reboot and fix boot " +
					"order to boot from disk /dev/test-disk (test-serial))",
				expectedStatusInfo: "Expected the host to boot from disk, but it booted the installation image - " +
					"please reboot and fix boot order to boot from disk /dev/test-disk (test-serial)",
				expectedRole:      models.HostRoleWorker,
				expectedInventory: defaultInventory(),
				hostKind:          models.HostKindAddToExistingClusterHost,
				origRole:          models.HostRoleWorker,
			},
		}

		for i := range tests {
			t := tests[i]

			It(fmt.Sprintf("register %s host in reboot", t.srcState), func() {
				Expect(db.Create(&models.Host{
					ID:                   &hostId,
					ClusterID:            clusterId,
					Role:                 t.origRole,
					Inventory:            defaultInventory(),
					Status:               swag.String(t.srcState),
					Progress:             &t.progress,
					InstallationDiskPath: GetDeviceFullName(defaultDisk.Name),
					Kind:                 &t.hostKind,
				}).Error).ShouldNot(HaveOccurred())

				if t.eventSeverity != "" && t.eventMessage != "" {
					mockEvents.EXPECT().AddEvent(
						gomock.Any(),
						clusterId,
						&hostId,
						t.eventSeverity,
						fmt.Sprintf(t.eventMessage, hostId.String()),
						gomock.Any())
				}

				Expect(hapi.RegisterHost(ctx, &models.Host{
					ID:        &hostId,
					ClusterID: clusterId,
					Status:    swag.String(t.srcState),
				})).ShouldNot(HaveOccurred())

				h := getHost(hostId, clusterId, db)
				Expect(swag.StringValue(h.Status)).Should(Equal(t.dstState))
				Expect(h.Role).Should(Equal(t.expectedRole))
				Expect(h.Inventory).Should(Equal(t.expectedInventory))
				Expect(swag.StringValue(h.StatusInfo)).Should(Equal(t.expectedStatusInfo))
			})
		}
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("HostInstallationFailed", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		ctrl              *gomock.Controller
		mockMetric        *metrics.MockAPI
		mockEvents        *events.MockHandler
		dbName            = "host_installation_failed"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), mockMetric, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(hostId, clusterId, "")
		host.Status = swag.String(models.HostStatusInstalling)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("handle_installation_error", func() {
		mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, models.EventSeverityError,
			fmt.Sprintf("Host %s: updated status from \"installing\" to \"error\" (installation command failed)", host.ID.String()),
			gomock.Any())
		mockMetric.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
		Expect(hapi.HandleInstallationFailure(ctx, &host)).ShouldNot(HaveOccurred())
		h := getHost(hostId, clusterId, db)
		Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusError))
		Expect(swag.StringValue(h.StatusInfo)).Should(Equal("installation command failed"))
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("Cancel host installation", func() {
	var (
		ctx               = context.Background()
		dbName            = "cancel_host_installation_test"
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		ctrl              *gomock.Controller
		mockEventsHandler *events.MockHandler
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEventsHandler = events.NewMockHandler(ctrl)
		hapi = NewManager(getTestLog(), db, mockEventsHandler, nil, nil, createValidatorCfg(), nil, defaultConfig, nil)
	})

	tests := []struct {
		state      string
		success    bool
		statusCode int32
	}{
		{state: models.HostStatusPreparingForInstallation, success: true},
		{state: models.HostStatusInstalling, success: true},
		{state: models.HostStatusInstallingInProgress, success: true},
		{state: models.HostStatusInstalled, success: true},
		{state: models.HostStatusError, success: true},
		{state: models.HostStatusDisabled, success: true},
		{state: models.HostStatusInstallingPendingUserAction, success: true},
		{state: models.HostStatusDiscovering, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusKnown, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusPendingForInput, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusResettingPendingUserAction, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusDisconnected, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusCancelled, success: false, statusCode: http.StatusConflict},
	}

	acceptNewEvents := func(times int) {
		mockEventsHandler.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(times)
	}

	for _, t := range tests {
		It(fmt.Sprintf("cancel from state %s", t.state), func() {
			hostId = strfmt.UUID(uuid.New().String())
			clusterId = strfmt.UUID(uuid.New().String())
			host = getTestHost(hostId, clusterId, "")
			host.Status = swag.String(t.state)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			eventsNum := 1
			if t.success {
				eventsNum++
			}
			acceptNewEvents(eventsNum)
			err := hapi.CancelInstallation(ctx, &host, "reason", db)
			h := getHost(hostId, clusterId, db)
			if t.success {
				Expect(err).ShouldNot(HaveOccurred())
				Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusCancelled))
			} else {
				Expect(err).Should(HaveOccurred())
				Expect(err.StatusCode()).Should(Equal(t.statusCode))
				Expect(swag.StringValue(h.Status)).Should(Equal(t.state))
			}
		})
	}

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("Reset host", func() {
	var (
		ctx               = context.Background()
		dbName            = "reset_host_test"
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		ctrl              *gomock.Controller
		mockEventsHandler *events.MockHandler
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEventsHandler = events.NewMockHandler(ctrl)
		hapi = NewManager(getTestLog(), db, mockEventsHandler, nil, nil, createValidatorCfg(), nil, defaultConfig, nil)
	})

	tests := []struct {
		state      string
		success    bool
		statusCode int32
	}{
		{state: models.HostStatusPreparingForInstallation, success: true},
		{state: models.HostStatusInstalling, success: true},
		{state: models.HostStatusInstallingInProgress, success: true},
		{state: models.HostStatusInstalled, success: true},
		{state: models.HostStatusError, success: true},
		{state: models.HostStatusDisabled, success: true},
		{state: models.HostStatusInstallingPendingUserAction, success: true},
		{state: models.HostStatusCancelled, success: true},
		{state: models.HostStatusDiscovering, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusKnown, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusPendingForInput, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusResettingPendingUserAction, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusDisconnected, success: false, statusCode: http.StatusConflict},
	}

	acceptNewEvents := func(times int) {
		mockEventsHandler.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(times)
	}

	for _, t := range tests {
		It(fmt.Sprintf("reset from state %s", t.state), func() {
			hostId = strfmt.UUID(uuid.New().String())
			clusterId = strfmt.UUID(uuid.New().String())
			host = getTestHost(hostId, clusterId, "")
			host.Status = swag.String(t.state)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			eventsNum := 1
			if t.success {
				eventsNum++
			}
			acceptNewEvents(eventsNum)
			err := hapi.ResetHost(ctx, &host, "reason", db)
			h := getHost(hostId, clusterId, db)
			if t.success {
				Expect(err).ShouldNot(HaveOccurred())
				Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusResetting))
			} else {
				Expect(err).Should(HaveOccurred())
				Expect(err.StatusCode()).Should(Equal(t.statusCode))
				Expect(swag.StringValue(h.Status)).Should(Equal(t.state))
			}
		})
	}

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("Install", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEvents        *events.MockHandler
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            = "transition_install"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("install host", func() {
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusInstalling))
			Expect(*h.StatusInfo).Should(Equal(statusInfoInstalling))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
		}

		noChange := func(reply error) {
			Expect(reply).To(BeNil())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusDisabled))
		}

		tests := []struct {
			name       string
			srcState   string
			validation func(error)
		}{
			{
				name:       "prepared",
				srcState:   models.HostStatusPreparingForInstallation,
				validation: success,
			},
			{
				name:       "known",
				srcState:   models.HostStatusKnown,
				validation: failure,
			},
			{
				name:       "disabled nothing change",
				srcState:   models.HostStatusDisabled,
				validation: noChange,
			},
			{
				name:       "disconnected",
				srcState:   models.HostStatusDisconnected,
				validation: failure,
			},
			{
				name:       "discovering",
				srcState:   models.HostStatusDiscovering,
				validation: failure,
			},
			{
				name:       "error",
				srcState:   models.HostStatusError,
				validation: failure,
			},
			{
				name:       "installed",
				srcState:   models.HostStatusInstalled,
				validation: failure,
			},
			{
				name:       "installing",
				srcState:   models.HostStatusInstalling,
				validation: failure,
			},
			{
				name:       "in-progress",
				srcState:   models.HostStatusInstallingInProgress,
				validation: failure,
			},
			{
				name:       "insufficient",
				srcState:   models.HostStatusInsufficient,
				validation: failure,
			},
			{
				name:       "resetting",
				srcState:   models.HostStatusResetting,
				validation: failure,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				host = getTestHost(hostId, clusterId, t.srcState)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, models.EventSeverityInfo,
					fmt.Sprintf("Host %s: updated status from \"%s\" to \"installing\" (Installation is in progress)", host.ID.String(), t.srcState),
					gomock.Any())
				t.validation(hapi.Install(ctx, &host, nil))
			})
		}
	})

	Context("install with transaction", func() {
		BeforeEach(func() {
			host = getTestHost(hostId, clusterId, models.HostStatusPreparingForInstallation)
			host.StatusInfo = swag.String(models.HostStatusPreparingForInstallation)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})

		It("success", func() {
			tx := db.Begin()
			Expect(tx.Error).To(BeNil())
			mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, models.EventSeverityInfo,
				fmt.Sprintf("Host %s: updated status from \"preparing-for-installation\" to \"installing\" (Installation is in progress)", host.ID.String()),
				gomock.Any())
			Expect(hapi.Install(ctx, &host, tx)).ShouldNot(HaveOccurred())
			Expect(tx.Commit().Error).ShouldNot(HaveOccurred())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusInstalling))
			Expect(*h.StatusInfo).Should(Equal(statusInfoInstalling))
		})

		It("rollback transition", func() {
			tx := db.Begin()
			Expect(tx.Error).To(BeNil())
			mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, models.EventSeverityInfo,
				fmt.Sprintf("Host %s: updated status from \"preparing-for-installation\" to \"installing\" (Installation is in progress)", host.ID.String()),
				gomock.Any())
			Expect(hapi.Install(ctx, &host, tx)).ShouldNot(HaveOccurred())
			Expect(tx.Rollback().Error).ShouldNot(HaveOccurred())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusPreparingForInstallation))
			Expect(*h.StatusInfo).Should(Equal(models.HostStatusPreparingForInstallation))
		})
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("Disable", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEvents        *events.MockHandler
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            = "transition_disable"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("disable host", func() {
		var srcState string
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusDisabled))
			Expect(*h.StatusInfo).Should(Equal(statusInfoDisabled))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(srcState))
		}

		mockEventsUpdateStatus := func(srcState string) {
			mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, models.EventSeverityInfo,
				fmt.Sprintf(`Host %s: updated status from "%s" to "disabled" (Host was manually disabled)`,
					host.ID.String(), srcState),
				gomock.Any()).Times(1)
		}

		tests := []struct {
			name       string
			srcState   string
			validation func(error)
			mocks      []func(string)
		}{
			{
				name:       "known",
				srcState:   models.HostStatusKnown,
				validation: success,
				mocks:      []func(string){mockEventsUpdateStatus},
			},
			{
				name:       "disabled nothing change",
				srcState:   models.HostStatusDisabled,
				validation: failure,
			},
			{
				name:       "disconnected",
				srcState:   models.HostStatusDisconnected,
				validation: success,
				mocks:      []func(string){mockEventsUpdateStatus},
			},
			{
				name:       "discovering",
				srcState:   models.HostStatusDiscovering,
				validation: success,
				mocks:      []func(string){mockEventsUpdateStatus},
			},
			{
				name:       "error",
				srcState:   models.HostStatusError,
				validation: failure,
			},
			{
				name:       "installed",
				srcState:   models.HostStatusInstalled,
				validation: failure,
			},
			{
				name:       "installing",
				srcState:   models.HostStatusInstalling,
				validation: failure,
			},
			{
				name:       "in-progress",
				srcState:   models.HostStatusInstallingInProgress,
				validation: failure,
			},
			{
				name:       "insufficient",
				srcState:   models.HostStatusInsufficient,
				validation: success,
				mocks:      []func(string){mockEventsUpdateStatus},
			},
			{
				name:       "resetting",
				srcState:   models.HostStatusResetting,
				validation: failure,
			},
			{
				name:       models.HostStatusPendingForInput,
				srcState:   models.HostStatusPendingForInput,
				validation: success,
				mocks:      []func(string){mockEventsUpdateStatus},
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				srcState = t.srcState
				host = getTestHost(hostId, clusterId, srcState)
				for _, m := range t.mocks {
					m(t.srcState)
				}
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				t.validation(hapi.DisableHost(ctx, &host, db))
			})
		}
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("Enable", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		ctrl              *gomock.Controller
		mockEvents        *events.MockHandler
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            = "transition_enable"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("enable host", func() {
		var srcState string
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusDiscovering))
			Expect(*h.StatusInfo).Should(Equal(statusInfoDiscovering))
			Expect(h.Inventory).Should(Equal(""))
		}

		failure := func(reply error) {
			Expect(reply).Should(HaveOccurred())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(srcState))
			Expect(h.Inventory).Should(Equal(defaultHwInfo))
		}

		tests := []struct {
			name       string
			srcState   string
			validation func(error)
			sendEvent  bool
		}{
			{
				name:       "known",
				srcState:   models.HostStatusKnown,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "disabled to enable",
				srcState:   models.HostStatusDisabled,
				validation: success,
				sendEvent:  true,
			},
			{
				name:       "disconnected",
				srcState:   models.HostStatusDisconnected,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "discovering",
				srcState:   models.HostStatusDiscovering,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "error",
				srcState:   models.HostStatusError,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "installed",
				srcState:   models.HostStatusInstalled,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "installing",
				srcState:   models.HostStatusInstalling,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "in-progress",
				srcState:   models.HostStatusInstallingInProgress,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "insufficient",
				srcState:   models.HostStatusInsufficient,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "resetting",
				srcState:   models.HostStatusResetting,
				validation: failure,
				sendEvent:  false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				srcState = t.srcState
				host = getTestHost(hostId, clusterId, srcState)
				host.Inventory = defaultHwInfo
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				if t.sendEvent {
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, models.EventSeverityInfo,
						fmt.Sprintf("Host %s: updated status from \"%s\" to \"discovering\" (Waiting for host to send hardware details)", hostutil.GetHostnameForMsg(&host), srcState),
						gomock.Any())
				}
				t.validation(hapi.EnableHost(ctx, &host, db))
			})
		}
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

type statusInfoChecker interface {
	check(statusInfo *string)
}

type valueChecker struct {
	value string
}

func (v *valueChecker) check(value *string) {
	if value == nil {
		Expect(v.value).To(Equal(""))
	} else {
		Expect(*value).To(Equal(v.value))
	}
}

func makeValueChecker(value string) statusInfoChecker {
	return &valueChecker{value: value}
}

type validationsChecker struct {
	expected map[validationID]validationCheckResult
}

func (j *validationsChecker) check(validationsStr string) {
	validationMap := make(map[string][]validationResult)
	Expect(json.Unmarshal([]byte(validationsStr), &validationMap)).ToNot(HaveOccurred())
next:
	for id, checkedResult := range j.expected {
		category, err := id.category()
		Expect(err).ToNot(HaveOccurred())
		results, ok := validationMap[category]
		Expect(ok).To(BeTrue())
		for _, r := range results {
			if r.ID == id {
				Expect(r.Status).To(Equal(checkedResult.status), "id = %s", id.String())
				Expect(r.Message).To(MatchRegexp(checkedResult.messagePattern))
				continue next
			}
		}
		// Should not reach here
		Expect(false).To(BeTrue())
	}
}

type validationCheckResult struct {
	status         validationStatus
	messagePattern string
}

func makeJsonChecker(expected map[validationID]validationCheckResult) *validationsChecker {
	return &validationsChecker{expected: expected}
}

var _ = Describe("Refresh Host", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		cluster           common.Cluster
		mockEvents        *events.MockHandler
		ctrl              *gomock.Controller
		dbName            string = "host_transition_test_refresh_host"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("host installation timeout", func() {
		var srcState = models.HostStatusInstalling
		timePassedTypes := map[string]time.Duration{
			"under_timeout": 5 * time.Minute,
			"over_timeout":  90 * time.Minute,
		}

		for passedTimeKey, passedTimeValue := range timePassedTypes {
			name := fmt.Sprintf("installing %s", passedTimeKey)
			It(name, func() {
				passedTimeKind := passedTimeKey
				passedTime := passedTimeValue
				hostCheckInAt := strfmt.DateTime(time.Now())
				host = getTestHost(hostId, clusterId, srcState)
				host.Inventory = masterInventory()
				host.Role = models.HostRoleMaster
				host.CheckedInAt = hostCheckInAt
				host.StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-passedTime))

				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				cluster = getTestCluster(clusterId, "1.2.3.0/24")
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
				if passedTimeKind == "over_timeout" {
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, hostutil.GetEventSeverityFromHostStatus(models.HostStatusError),
						gomock.Any(), gomock.Any())
				}
				err := hapi.RefreshStatus(ctx, &host, db)

				Expect(err).ToNot(HaveOccurred())
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())

				if passedTimeKind == "under_timeout" {
					Expect(swag.StringValue(resultHost.Status)).To(Equal(models.HostStatusInstalling))
				} else {
					Expect(swag.StringValue(resultHost.Status)).To(Equal(models.HostStatusError))
					Expect(swag.StringValue(resultHost.StatusInfo)).To(Equal("Host failed to install due to timeout while starting installation"))
				}
			})
		}
	})

	Context("host installationInProgress timeout", func() {
		var srcState string
		var invalidStage models.HostStage = "not_mentioned_stage"

		installationStages := []models.HostStage{
			models.HostStageStartingInstallation,
			models.HostStageWritingImageToDisk,
			models.HostStageRebooting,
			models.HostStageConfiguring,
			models.HostStageWaitingForIgnition,
			models.HostStageInstalling,
			invalidStage,
		}
		timePassedTypes := map[string]time.Duration{
			"under_timeout": 5 * time.Minute,
			"over_timeout":  90 * time.Minute,
		}

		for j := range installationStages {
			stage := installationStages[j]
			for passedTimeKey, passedTimeValue := range timePassedTypes {
				name := fmt.Sprintf("installationInProgress stage %s %s", stage, passedTimeKey)
				passedTimeKind := passedTimeKey
				passedTime := passedTimeValue
				It(name, func() {
					hostCheckInAt := strfmt.DateTime(time.Now())
					srcState = models.HostStatusInstallingInProgress
					host = getTestHost(hostId, clusterId, srcState)
					host.Inventory = masterInventory()
					host.Role = models.HostRoleMaster
					host.CheckedInAt = hostCheckInAt

					progress := models.HostProgressInfo{
						CurrentStage:   stage,
						StageStartedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
						StageUpdatedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
					}

					host.Progress = &progress

					Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
					cluster = getTestCluster(clusterId, "1.2.3.0/24")
					Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

					if passedTimeKind == "over_timeout" {
						mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, hostutil.GetEventSeverityFromHostStatus(models.HostStatusError),
							gomock.Any(), gomock.Any())
					}
					err := hapi.RefreshStatus(ctx, &host, db)

					Expect(err).ToNot(HaveOccurred())
					var resultHost models.Host
					Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())

					if passedTimeKind == "under_timeout" {
						Expect(swag.StringValue(resultHost.Status)).To(Equal(models.HostStatusInstallingInProgress))
					} else {
						Expect(swag.StringValue(resultHost.Status)).To(Equal(models.HostStatusError))
						timeFormat := InstallationProgressTimeout[stage].String()
						info := fmt.Sprintf("Host failed to install because its installation stage %s took longer than expected %s", stage, timeFormat)
						Expect(swag.StringValue(resultHost.StatusInfo)).To(Equal(info))
					}

				})
			}
		}
	})

	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name               string
			srcState           string
			inventory          string
			role               models.HostRole
			machineNetworkCidr string
			validCheckInTime   bool
			dstState           string
			statusInfoChecker  statusInfoChecker
			validationsChecker *validationsChecker
			connectivity       string
			errorExpected      bool
		}{
			{
				name:              "discovering to disconnected",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  false,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationFailure, messagePattern: "Host is disconnected"},
					HasInventory:         {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:       {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:         {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:     {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "insufficient to disconnected",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  false,
				srcState:          models.HostStatusInsufficient,
				dstState:          models.HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationFailure, messagePattern: "Host is disconnected"},
					HasInventory:         {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:       {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:         {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:     {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "known to disconnected",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  false,
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				errorExpected:     false,
			},
			{
				name:              "pending to disconnected",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  false,
				srcState:          models.HostStatusPendingForInput,
				dstState:          models.HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationFailure, messagePattern: "Host is disconnected"},
					HasInventory:         {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:       {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:         {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:     {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "disconnected to disconnected",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  false,
				srcState:          models.HostStatusDisconnected,
				dstState:          models.HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationFailure, messagePattern: "Host is disconnected"},
					HasInventory:         {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:       {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:         {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:     {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "disconnected to discovering",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  true,
				srcState:          models.HostStatusDisconnected,
				dstState:          models.HostStatusDiscovering,
				statusInfoChecker: makeValueChecker(statusInfoDiscovering),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:       {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:         {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:     {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "discovering to discovering",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  true,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusDiscovering,
				statusInfoChecker: makeValueChecker(statusInfoDiscovering),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:       {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:         {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:     {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "disconnected to insufficient (1)",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  true,
				srcState:          models.HostStatusDisconnected,
				dstState:          models.HostStatusInsufficient,
				statusInfoChecker: makeValueChecker(statusInfoInsufficientHardware),
				inventory:         insufficientHWInventory(),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "Require at least 8 GiB RAM, found only 0 GiB"},
					HasMinValidDisks:     {status: ValidationFailure, messagePattern: "Require a disk of at least 120 GB"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role auto-assign"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 8 GiB RAM role auto-assign, found only 0"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "insufficient to insufficient (1)",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  true,
				srcState:          models.HostStatusInsufficient,
				dstState:          models.HostStatusInsufficient,
				statusInfoChecker: makeValueChecker(statusInfoInsufficientHardware),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "Require at least 8 GiB RAM, found only 0 GiB"},
					HasMinValidDisks:     {status: ValidationFailure, messagePattern: "Require a disk of at least 120 GB"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role auto-assign"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 8 GiB RAM role auto-assign, found only 0"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				inventory:     insufficientHWInventory(),
				errorExpected: false,
			},
			{
				name:              "discovering to insufficient (1)",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  true,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusInsufficient,
				statusInfoChecker: makeValueChecker(statusInfoInsufficientHardware),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "Require at least 8 GiB RAM, found only 0 GiB"},
					HasMinValidDisks:     {status: ValidationFailure, messagePattern: "Require a disk of at least 120 GB"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 8 GiB RAM role"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				inventory:     insufficientHWInventory(),
				errorExpected: false,
			},
			{
				name:              "pending to insufficient (1)",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  true,
				srcState:          models.HostStatusPendingForInput,
				dstState:          models.HostStatusPendingForInput,
				statusInfoChecker: makeValueChecker(""),
				inventory:         insufficientHWInventory(),
				errorExpected:     true,
			},
			{
				name:              "known to insufficient (1)",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  true,
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusKnown,
				statusInfoChecker: makeValueChecker(""),
				inventory:         insufficientHWInventory(),
				errorExpected:     true,
			},
			{
				name:              "known to pending",
				validCheckInTime:  true,
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusPendingForInput,
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				inventory:     workerInventory(),
				errorExpected: false,
			},
			{
				name:              "pending to pending",
				validCheckInTime:  true,
				srcState:          models.HostStatusPendingForInput,
				dstState:          models.HostStatusPendingForInput,
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				inventory:     workerInventory(),
				errorExpected: false,
			},
			{
				name:               "disconnected to insufficient (2)",
				validCheckInTime:   true,
				srcState:           models.HostStatusDisconnected,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "5.6.7.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationFailure, messagePattern: "Host does not belong to machine network CIDR "},
				}),
				inventory:     workerInventory(),
				errorExpected: false,
			},
			{
				name:               "discovering to insufficient (2)",
				validCheckInTime:   true,
				srcState:           models.HostStatusDiscovering,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "5.6.7.0/24",
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationFailure, messagePattern: "Require at least 4 CPU cores for master role, found only 2"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 16 GiB RAM role master, found only 8"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationFailure, messagePattern: "Host does not belong to machine network CIDR "},
				}),
				inventory:     workerInventory(),
				errorExpected: false,
			},
			{
				name:               "insufficient to insufficient (2)",
				validCheckInTime:   true,
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationFailure, messagePattern: "Require at least 4 CPU cores for master role, found only 2"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 16 GiB RAM role master, found only 8"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:     workerInventory(),
				errorExpected: false,
			},
			{
				name:               "pending to insufficient (2)",
				validCheckInTime:   true,
				srcState:           models.HostStatusPendingForInput,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				inventory:          workerInventory(),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationFailure, messagePattern: "Require at least 4 CPU cores for master role, found only 2"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 16 GiB RAM role master, found only 8"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:               "known to insufficient (2)",
				validCheckInTime:   true,
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "5.6.7.0/24",
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationFailure, messagePattern: "Host does not belong to machine network CIDR"},
				}),
				inventory:     masterInventory(),
				errorExpected: false,
			},
			{
				name:               "insufficient to insufficient (2)",
				validCheckInTime:   true,
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "5.6.7.0/24",
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationFailure, messagePattern: "Host does not belong to machine network CIDR"},
				}),
				inventory:     masterInventory(),
				errorExpected: false,
			},
			{
				name:               "insufficient to insufficient (localhost)",
				validCheckInTime:   true,
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsHostnameValid:      {status: ValidationFailure, messagePattern: "Hostname localhost is forbidden"},
				}),
				inventory:     masterInventoryWithHostname("localhost"),
				errorExpected: false,
			},
			{
				name:               "discovering to known",
				validCheckInTime:   true,
				srcState:           models.HostStatusDiscovering,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsHostnameValid:      {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsAPIVipConnected:    {status: ValidationSuccess, messagePattern: "API VIP connectivity success"},
				}),
				inventory:     masterInventory(),
				errorExpected: false,
			},
			{
				name:               "insufficient to known",
				validCheckInTime:   true,
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsHostnameValid:      {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
				}),
				inventory:     masterInventory(),
				errorExpected: false,
			},
			{
				name:               "pending to known",
				validCheckInTime:   true,
				srcState:           models.HostStatusPendingForInput,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsHostnameValid:      {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
				}),
				inventory:     masterInventory(),
				errorExpected: false,
			},
			{
				name:               "known to known",
				validCheckInTime:   true,
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:            {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:           {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:         {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:           {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:       {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:   {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:     {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:       {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:       {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					BelongsToMajorityGroup: {status: ValidationSuccess, messagePattern: "Host has connectivity to the majority of hosts in the cluster"},
				}),
				inventory:     masterInventory(),
				errorExpected: false,
			},
			{
				name:               "known to insufficient",
				validCheckInTime:   true,
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:            {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:           {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:         {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:           {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:       {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:   {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:     {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:       {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:       {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					BelongsToMajorityGroup: {status: ValidationFailure, messagePattern: "No connectivity to the majority of hosts in the cluster"},
				}),
				inventory:     masterInventory(),
				connectivity:  fmt.Sprintf("{\"%s\":[]}", "1.2.3.0/24"),
				errorExpected: false,
			},
			{
				name:               "known to known with unexpected role",
				validCheckInTime:   true,
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "kuku",
				statusInfoChecker:  makeValueChecker(""),
				inventory:          masterInventory(),
				errorExpected:      true,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				hostCheckInAt := strfmt.DateTime(time.Now())
				if !t.validCheckInTime {
					// Timeout for checkin is 3 minutes so subtract 4 minutes from the current time
					hostCheckInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
				}
				srcState = t.srcState
				host = getTestHost(hostId, clusterId, srcState)
				host.Inventory = t.inventory
				host.Role = t.role
				host.CheckedInAt = hostCheckInAt
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				cluster = getTestCluster(clusterId, t.machineNetworkCidr)
				if t.connectivity == "" {
					cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", t.machineNetworkCidr, hostId.String())
				} else {
					cluster.ConnectivityMajorityGroups = t.connectivity
				}
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
				if srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, hostutil.GetEventSeverityFromHostStatus(t.dstState),
						gomock.Any(), gomock.Any())
				}
				err := hapi.RefreshStatus(ctx, &host, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Role).To(Equal(t.role))
				Expect(resultHost.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(resultHost.StatusInfo)
				if t.validationsChecker != nil {
					t.validationsChecker.check(resultHost.ValidationsInfo)
				}
			})
		}
	})
	Context("Pending timed out", func() {
		tests := []struct {
			name          string
			clusterStatus string
			dstState      string
			statusInfo    string
			errorExpected bool
		}{
			{
				name:          "No timeout",
				dstState:      models.HostStatusPreparingForInstallation,
				statusInfo:    "",
				clusterStatus: models.ClusterStatusPreparingForInstallation,
			},
			{
				name:          "Timeout",
				dstState:      models.HostStatusError,
				statusInfo:    statusInfoPreparingTimedOut,
				clusterStatus: models.ClusterStatusInstalled,
			},
		}
		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				host = getTestHost(hostId, clusterId, models.HostStatusPreparingForInstallation)
				host.Inventory = masterInventory()
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				cluster = getTestCluster(clusterId, "1.2.3.0/24")
				cluster.Status = &t.clusterStatus
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
				if *host.Status != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, hostutil.GetEventSeverityFromHostStatus(t.dstState),
						gomock.Any(), gomock.Any())
				}
				err := hapi.RefreshStatus(ctx, &host, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(swag.StringValue(resultHost.Status)).To(Equal(t.dstState))
				Expect(swag.StringValue(resultHost.StatusInfo)).To(Equal(t.statusInfo))
			})
		}

	})
	Context("Unique hostname", func() {
		var srcState string
		var otherHostID strfmt.UUID

		BeforeEach(func() {
			otherHostID = strfmt.UUID(uuid.New().String())
		})

		tests := []struct {
			name                   string
			srcState               string
			inventory              string
			role                   models.HostRole
			machineNetworkCidr     string
			dstState               string
			requestedHostname      string
			otherState             string
			otherRequestedHostname string
			otherInventory         string
			statusInfoChecker      statusInfoChecker
			validationsChecker     *validationsChecker
			errorExpected          bool
		}{
			{
				name:               "insufficient to known",
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsAPIVipConnected:    {status: ValidationSuccess, messagePattern: "API VIP connectivity success"},
				}),
				inventory:      masterInventoryWithHostname("first"),
				otherState:     models.HostStatusInsufficient,
				otherInventory: masterInventoryWithHostname("second"),
				errorExpected:  false,
			},
			{
				name:               "insufficient to insufficient (same hostname) 1",
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:      masterInventoryWithHostname("first"),
				otherState:     models.HostStatusInsufficient,
				otherInventory: masterInventoryWithHostname("first"),
				errorExpected:  false,
			},
			{
				name:               "insufficient to insufficient (same hostname) 2",
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:              masterInventoryWithHostname("first"),
				otherState:             models.HostStatusInsufficient,
				otherInventory:         masterInventoryWithHostname("second"),
				otherRequestedHostname: "first",
				errorExpected:          false,
			},
			{
				name:               "insufficient to insufficient (same hostname) 3",
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:         masterInventoryWithHostname("first"),
				requestedHostname: "second",
				otherState:        models.HostStatusInsufficient,
				otherInventory:    masterInventoryWithHostname("second"),
				errorExpected:     false,
			},
			{
				name:               "insufficient to insufficient (same hostname) 4",
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:              masterInventoryWithHostname("first"),
				requestedHostname:      "third",
				otherState:             models.HostStatusInsufficient,
				otherInventory:         masterInventoryWithHostname("second"),
				otherRequestedHostname: "third",
				errorExpected:          false,
			},
			{
				name:               "insufficient to known 2",
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsAPIVipConnected:    {status: ValidationSuccess, messagePattern: "API VIP connectivity success"},
				}),
				inventory:              masterInventoryWithHostname("first"),
				requestedHostname:      "third",
				otherState:             models.HostStatusInsufficient,
				otherInventory:         masterInventoryWithHostname("second"),
				otherRequestedHostname: "forth",
				errorExpected:          false,
			},

			{
				name:               "known to known",
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:      masterInventoryWithHostname("first"),
				otherState:     models.HostStatusInsufficient,
				otherInventory: masterInventoryWithHostname("second"),
				errorExpected:  false,
			},
			{
				name:               "known to insufficient (same hostname) 1",
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:      masterInventoryWithHostname("first"),
				otherState:     models.HostStatusInsufficient,
				otherInventory: masterInventoryWithHostname("first"),
				errorExpected:  false,
			},
			{
				name:               "known to insufficient (same hostname) 2",
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:              masterInventoryWithHostname("first"),
				otherState:             models.HostStatusInsufficient,
				otherInventory:         masterInventoryWithHostname("second"),
				otherRequestedHostname: "first",
				errorExpected:          false,
			},
			{
				name:               "known to insufficient (same hostname) 3",
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:         masterInventoryWithHostname("first"),
				requestedHostname: "second",
				otherState:        models.HostStatusInsufficient,
				otherInventory:    masterInventoryWithHostname("second"),
				errorExpected:     false,
			},
			{
				name:               "known to insufficient (same hostname) 4",
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:              masterInventoryWithHostname("first"),
				requestedHostname:      "third",
				otherState:             models.HostStatusInsufficient,
				otherInventory:         masterInventoryWithHostname("second"),
				otherRequestedHostname: "third",
				errorExpected:          false,
			},
			{
				name:               "known to known 2",
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:              masterInventoryWithHostname("first"),
				requestedHostname:      "third",
				otherState:             models.HostStatusInsufficient,
				otherInventory:         masterInventoryWithHostname("first"),
				otherRequestedHostname: "forth",
				errorExpected:          false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				srcState = t.srcState
				host = getTestHost(hostId, clusterId, srcState)
				host.Inventory = t.inventory
				host.Role = t.role
				host.CheckedInAt = strfmt.DateTime(time.Now())
				host.RequestedHostname = t.requestedHostname
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				otherHost := getTestHost(otherHostID, clusterId, t.otherState)
				otherHost.RequestedHostname = t.otherRequestedHostname
				otherHost.Inventory = t.otherInventory
				Expect(db.Create(&otherHost).Error).ShouldNot(HaveOccurred())
				cluster = getTestCluster(clusterId, t.machineNetworkCidr)
				cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", t.machineNetworkCidr, hostId.String())
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
				expectedSeverity := models.EventSeverityInfo
				if t.dstState == models.HostStatusInsufficient {
					expectedSeverity = models.EventSeverityWarning
				}
				if !t.errorExpected && srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, expectedSeverity,
						gomock.Any(), gomock.Any())
				}

				err := hapi.RefreshStatus(ctx, &host, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Role).To(Equal(t.role))
				Expect(resultHost.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(resultHost.StatusInfo)
				if t.validationsChecker != nil {
					t.validationsChecker.check(resultHost.ValidationsInfo)
				}
			})
		}
	})
	Context("Cluster Errors", func() {
		for _, srcState := range []string{
			models.HostStatusInstalling,
			models.HostStatusInstallingInProgress,
			models.HostStatusInstalled,
		} {
			It(fmt.Sprintf("host src: %s cluster error: false", srcState), func() {
				h := getTestHost(hostId, clusterId, srcState)
				h.Inventory = masterInventory()
				Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
				c := getTestCluster(clusterId, "1.2.3.0/24")
				c.Status = swag.String(models.ClusterStatusInstalling)
				Expect(db.Create(&c).Error).ToNot(HaveOccurred())
				err := hapi.RefreshStatus(ctx, &h, db)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(swag.StringValue(h.Status)).Should(Equal(srcState))
			})
			It(fmt.Sprintf("host src: %s cluster error: true", srcState), func() {
				h := getTestHost(hostId, clusterId, srcState)
				h.Inventory = masterInventory()
				Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
				c := getTestCluster(clusterId, "1.2.3.0/24")
				c.Status = swag.String(models.ClusterStatusError)
				Expect(db.Create(&c).Error).ToNot(HaveOccurred())
				mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, &hostId, models.EventSeverityError,
					"Host master-hostname: updated status from \"installed\" to \"error\" (Host is part of a cluster that failed to install)",
					gomock.Any())
				err := hapi.RefreshStatus(ctx, &h, db)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusError))
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(swag.StringValue(resultHost.StatusInfo)).Should(Equal(statusInfoAbortingDueClusterErrors))
			})
		}
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})
