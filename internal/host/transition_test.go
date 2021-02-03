package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/thoas/go-funk"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
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
		mockHwValidator := hardware.NewMockValidator(ctrl)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	It("register_new", func() {
		Expect(hapi.RegisterHost(ctx, &models.Host{ID: &hostId, ClusterID: clusterId, DiscoveryAgentVersion: "v1.0.1"}, db)).ShouldNot(HaveOccurred())
		h := hostutil.GetHostFromDB(hostId, clusterId, db)
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
				},
					db)

				if t.errorCode == 0 {
					Expect(err).ShouldNot(HaveOccurred())
				} else {
					Expect(err).Should(HaveOccurred())
					serr, ok := err.(*common.ApiErrorResponse)
					Expect(ok).Should(Equal(true))
					Expect(serr.StatusCode()).Should(Equal(t.errorCode))
				}
				h := hostutil.GetHostFromDB(hostId, clusterId, db)
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

	It("register disabled host", func() {
		Expect(db.Create(&models.Host{
			ID:        &hostId,
			ClusterID: clusterId,
			Role:      models.HostRoleMaster,
			Inventory: defaultHwInfo,
			Status:    swag.String(models.HostStatusDisabled),
		}).Error).ShouldNot(HaveOccurred())

		Expect(hapi.RegisterHost(ctx, &models.Host{
			ID:                    &hostId,
			ClusterID:             clusterId,
			Status:                swag.String(models.HostStatusDisabled),
			DiscoveryAgentVersion: "v2.0.5",
		},
			db)).ShouldNot(HaveOccurred())
	})

	It("register host in error state", func() {
		Expect(db.Create(&models.Host{
			ID:        &hostId,
			ClusterID: clusterId,
			Role:      models.HostRoleMaster,
			Inventory: defaultHwInfo,
			Status:    swag.String(models.HostStatusError),
		}).Error).ShouldNot(HaveOccurred())

		Expect(hapi.RegisterHost(ctx, &models.Host{
			ID:                    &hostId,
			ClusterID:             clusterId,
			Status:                swag.String(models.HostStatusError),
			DiscoveryAgentVersion: "v2.0.5",
		},
			db)).ShouldNot(HaveOccurred())
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
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusDiscovering))
			Expect(h.Role).Should(Equal(models.HostRoleMaster))
			Expect(h.DiscoveryAgentVersion).To(Equal(discoveryAgentVersion))
			Expect(h.Bootstrap).To(BeFalse())

			// Verify resetted fields
			Expect(h.Inventory).To(BeEmpty())
			Expect(h.Progress.CurrentStage).To(BeEmpty())
			Expect(h.Progress.ProgressInfo).To(BeEmpty())
			Expect(h.NtpSources).To(BeEmpty())
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
					Bootstrap: true,
					Progress: &models.HostProgressInfo{
						CurrentStage: common.TestDefaultConfig.HostProgressStage,
						ProgressInfo: "some info",
					},
					NtpSources: "some ntp sources",
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
				},
					db)).ShouldNot(HaveOccurred())
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
				},
					db)).Should(HaveOccurred())

				h := hostutil.GetHostFromDB(hostId, clusterId, db)
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
				expectedInventory: common.GenerateTestDefaultInventory(),
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
				expectedInventory: common.GenerateTestDefaultInventory(),
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
				expectedInventory: common.GenerateTestDefaultInventory(),
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
					Inventory:            common.GenerateTestDefaultInventory(),
					Status:               swag.String(t.srcState),
					Progress:             &t.progress,
					InstallationDiskPath: hostutil.GetDeviceFullName(common.TestDefaultConfig.Disks.Name),
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
				},
					db)).ShouldNot(HaveOccurred())

				h := hostutil.GetHostFromDB(hostId, clusterId, db)
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
		mockHwValidator := hardware.NewMockValidator(ctrl)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), mockMetric, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(hostId, clusterId, "")
		host.Status = swag.String(models.HostStatusInstalling)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("handle_installation_error", func() {
		mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, models.EventSeverityError,
			fmt.Sprintf("Host %s: updated status from \"installing\" to \"error\" (installation command failed)", host.ID.String()),
			gomock.Any())
		mockMetric.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
		Expect(hapi.HandleInstallationFailure(ctx, &host)).ShouldNot(HaveOccurred())
		h := hostutil.GetHostFromDB(hostId, clusterId, db)
		Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusError))
		Expect(swag.StringValue(h.StatusInfo)).Should(Equal("installation command failed"))
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("RegisterInstalledOCPHost", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		ctrl              *gomock.Controller
		mockMetric        *metrics.MockAPI
		mockEvents        *events.MockHandler
		dbName            = "register_installed_host"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), mockMetric, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(hostId, clusterId, "")
	})

	It("register_installed_host", func() {
		Expect(hapi.RegisterInstalledOCPHost(ctx, &host, db)).ShouldNot(HaveOccurred())
		h := hostutil.GetHostFromDB(hostId, clusterId, db)
		Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusInstalled))
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
		mockHwValidator := hardware.NewMockValidator(ctrl)
		hapi = NewManager(common.GetTestLog(), db, mockEventsHandler, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil)
	})

	tests := []struct {
		state       string
		success     bool
		changeState bool
		statusCode  int32
	}{
		{state: models.HostStatusPreparingForInstallation, success: true, changeState: true},
		{state: models.HostStatusInstalling, success: true, changeState: true},
		{state: models.HostStatusInstallingInProgress, success: true, changeState: true},
		{state: models.HostStatusInstalled, success: true, changeState: true},
		{state: models.HostStatusError, success: true, changeState: true},
		{state: models.HostStatusDisabled, success: true, changeState: false},
		{state: models.HostStatusInstallingPendingUserAction, success: true, changeState: true},
		{state: models.HostStatusDiscovering, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusKnown, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusPendingForInput, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusResettingPendingUserAction, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusDisconnected, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusCancelled, success: false, statusCode: http.StatusConflict, changeState: false},
	}

	acceptNewEvents := func(times int) {
		mockEventsHandler.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(times)
	}

	for _, t := range tests {
		t := t
		It(fmt.Sprintf("cancel from state %s", t.state), func() {
			hostId = strfmt.UUID(uuid.New().String())
			clusterId = strfmt.UUID(uuid.New().String())
			host = hostutil.GenerateTestHost(hostId, clusterId, "")
			host.Status = swag.String(t.state)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			eventsNum := 0
			if t.changeState {
				eventsNum = 2
			}
			if !t.success {
				eventsNum = 1
			}
			acceptNewEvents(eventsNum)
			err := hapi.CancelInstallation(ctx, &host, "reason", db)
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			if t.success {
				Expect(err).ShouldNot(HaveOccurred())

				expectedState := models.HostStatusCancelled
				if !t.changeState {
					expectedState = t.state
				}

				Expect(swag.StringValue(h.Status)).Should(Equal(expectedState))
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
		mockHwValidator := hardware.NewMockValidator(ctrl)
		hapi = NewManager(common.GetTestLog(), db, mockEventsHandler, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil)
	})

	tests := []struct {
		state       string
		success     bool
		changeState bool
		statusCode  int32
	}{
		{state: models.HostStatusPreparingForInstallation, success: true, changeState: true},
		{state: models.HostStatusInstalling, success: true, changeState: true},
		{state: models.HostStatusInstallingInProgress, success: true, changeState: true},
		{state: models.HostStatusInstalled, success: true, changeState: true},
		{state: models.HostStatusError, success: true, changeState: true},
		{state: models.HostStatusDisabled, success: true, changeState: false},
		{state: models.HostStatusInstallingPendingUserAction, success: true, changeState: true},
		{state: models.HostStatusCancelled, success: true, changeState: true},
		{state: models.HostStatusAddedToExistingCluster, success: true, changeState: true},
		{state: models.HostStatusDiscovering, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusKnown, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusPendingForInput, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusResettingPendingUserAction, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusDisconnected, success: false, statusCode: http.StatusConflict, changeState: false},
	}

	acceptNewEvents := func(times int) {
		mockEventsHandler.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(times)
	}

	for _, t := range tests {
		t := t
		It(fmt.Sprintf("reset from state %s", t.state), func() {
			hostId = strfmt.UUID(uuid.New().String())
			clusterId = strfmt.UUID(uuid.New().String())
			host = hostutil.GenerateTestHost(hostId, clusterId, "")
			host.Status = swag.String(t.state)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			eventsNum := 0
			if t.changeState {
				eventsNum = 2
			}
			if !t.success {
				eventsNum = 1
			}
			acceptNewEvents(eventsNum)
			err := hapi.ResetHost(ctx, &host, "reason", db)
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			if t.success {
				Expect(err).ShouldNot(HaveOccurred())

				expectedState := models.HostStatusResetting
				if !t.changeState {
					expectedState = t.state
				}

				Expect(swag.StringValue(h.Status)).Should(Equal(expectedState))
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
		mockHwValidator := hardware.NewMockValidator(ctrl)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("install host", func() {
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusInstalling))
			Expect(*h.StatusInfo).Should(Equal(statusInfoInstalling))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
		}

		noChange := func(reply error) {
			Expect(reply).To(BeNil())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
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
				host = hostutil.GenerateTestHost(hostId, clusterId, t.srcState)
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
			host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusPreparingForInstallation)
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
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
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
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
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
		mockHwValidator := hardware.NewMockValidator(ctrl)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("disable host", func() {
		var srcState string
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusDisabled))
			Expect(*h.StatusInfo).Should(Equal(statusInfoDisabled))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
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
				host = hostutil.GenerateTestHost(hostId, clusterId, srcState)
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
		mockHwValidator := hardware.NewMockValidator(ctrl)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("enable host", func() {
		var srcState string
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusDiscovering))
			Expect(*h.StatusInfo).Should(Equal(statusInfoDiscovering))
			Expect(h.Inventory).Should(BeEmpty())
			Expect(h.Bootstrap).Should(BeFalse())
			Expect(h.NtpSources).Should(BeEmpty())
		}

		failure := func(reply error) {
			Expect(reply).Should(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(srcState))
			Expect(h.Inventory).Should(Equal(defaultHwInfo))
			Expect(h.Bootstrap).Should(Equal(true))

			var ntpSources []*models.NtpSource
			Expect(json.Unmarshal([]byte(h.NtpSources), &ntpSources)).ShouldNot(HaveOccurred())
			Expect(ntpSources).Should(Equal(defaultNTPSources))
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
				// Test setup - Host creation
				srcState = t.srcState
				host = hostutil.GenerateTestHost(hostId, clusterId, srcState)
				host.Inventory = defaultHwInfo
				host.Bootstrap = true

				bytes, err := json.Marshal(defaultNTPSources)
				Expect(err).ShouldNot(HaveOccurred())
				host.NtpSources = string(bytes)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

				// Test definition
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
	status         ValidationStatus
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
		mockHwValidator := hardware.NewMockValidator(ctrl)
		validatorCfg := createValidatorCfg()
		mockHwValidator.EXPECT().ListEligibleDisks(gomock.Any()).DoAndReturn(func(inventory *models.Inventory) []*models.Disk {
			// Mock the hwValidator behavior of performing simple filtering according to disk size, because these tests
			// rely on small disks to get filtered out.
			return funk.Filter(inventory.Disks, func(disk *models.Disk) bool {
				return disk.SizeBytes >= hardware.GibToBytes(validatorCfg.MinDiskSizeGb)
			}).([]*models.Disk)
		}).AnyTimes()
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, nil, defaultConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("host installation timeout - cluster is pending user action", func() {
		tests := []struct {
			stage         models.HostStage
			expectTimeout bool
		}{
			{models.HostStageRebooting, false},
			{models.HostStageStartWaitingForControlPlane, false},
			{models.HostStageWaitingForControlPlane, false},
			{models.HostStageStartingInstallation, true},
			{models.HostStageInstalling, true},
			{models.HostStageWritingImageToDisk, true},
			{models.HostStageConfiguring, true},
			{models.HostStageDone, true},
			{models.HostStageJoined, true},
			{models.HostStageWaitingForIgnition, false},
			{models.HostStageFailed, true},
		}

		passedTime := 90 * time.Minute

		for _, t := range tests {
			t := t
			It(fmt.Sprintf("checking timeout from stage %s", t.stage), func() {
				hostCheckInAt := strfmt.DateTime(time.Now())
				host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusInstallingInProgress)
				host.Inventory = hostutil.GenerateMasterInventory()
				host.Role = models.HostRoleMaster
				host.CheckedInAt = hostCheckInAt
				updatedAt := strfmt.DateTime(time.Now().Add(-passedTime))
				host.StatusUpdatedAt = updatedAt
				host.Progress = &models.HostProgressInfo{
					StageUpdatedAt: updatedAt,
					CurrentStage:   t.stage,
				}
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				cluster = hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
				cluster.Status = swag.String(models.ClusterStatusInstallingPendingUserAction)
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				if t.expectTimeout {
					mockEvents.EXPECT().AddEvent(
						gomock.Any(),
						host.ClusterID,
						&hostId,
						hostutil.GetEventSeverityFromHostStatus(models.HostStatusError),
						gomock.Any(),
						gomock.Any())
				}
				err := hapi.RefreshStatus(ctx, &host, db)
				Expect(err).ShouldNot(HaveOccurred())
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
				prevStageUpdatedAt := time.Time(updatedAt).UTC().Round(time.Second).String()
				currStageUpdateAt := time.Time(resultHost.Progress.StageUpdatedAt).UTC().Round(time.Second).String()
				if t.expectTimeout {
					Expect(prevStageUpdatedAt).Should(Equal(currStageUpdateAt))
					Expect(swag.StringValue(resultHost.Status)).Should(Equal(models.HostStatusError))
					Expect(swag.StringValue(resultHost.StatusInfo)).Should(Equal(formatProgressTimedOutInfo(t.stage)))
				} else {
					if funk.Contains(WrongBootOrderIgnoreTimeoutStages, t.stage) {
						Expect(prevStageUpdatedAt).ShouldNot(Equal(currStageUpdateAt))
					}
					Expect(swag.StringValue(resultHost.Status)).Should(Equal(models.HostStatusInstallingInProgress))
				}
			})
		}
	})

	Context("host disconnected & installation timeout", func() {
		var srcState string

		installationStages := []models.HostStage{
			models.HostStageInstalling,
			models.HostStageWritingImageToDisk,
		}

		for j := range installationStages {
			stage := installationStages[j]
			name := fmt.Sprintf("installationInProgress stage %s", stage)
			passedTime := 90 * time.Minute
			It(name, func() {
				srcState = models.HostStatusInstallingInProgress
				host = hostutil.GenerateTestHost(hostId, clusterId, srcState)
				host.Inventory = hostutil.GenerateMasterInventory()
				host.Role = models.HostRoleMaster
				host.CheckedInAt = strfmt.DateTime(time.Now().Add(-MaxHostDisconnectionTime - time.Minute))

				progress := models.HostProgressInfo{
					CurrentStage:   stage,
					StageStartedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
					StageUpdatedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
				}

				host.Progress = &progress

				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				cluster = hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, hostutil.GetEventSeverityFromHostStatus(models.HostStatusError),
					gomock.Any(), gomock.Any())
				err := hapi.RefreshStatus(ctx, &host, db)

				Expect(err).ToNot(HaveOccurred())
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())

				Expect(swag.StringValue(resultHost.Status)).To(Equal(models.HostStatusError))
				info := statusInfoConnectionTimedOut
				Expect(swag.StringValue(resultHost.StatusInfo)).To(MatchRegexp(info))

			})

		}
	})

	Context("host installation timeout", func() {
		var srcState = models.HostStatusInstalling
		timePassedTypes := map[string]time.Duration{
			"under_timeout": 5 * time.Minute,
			"over_timeout":  90 * time.Minute,
		}

		for passedTimeKey, passedTimeValue := range timePassedTypes {
			name := fmt.Sprintf("installing %s", passedTimeKey)
			passedTimeKey := passedTimeKey
			passedTimeValue := passedTimeValue
			It(name, func() {
				passedTimeKind := passedTimeKey
				passedTime := passedTimeValue
				hostCheckInAt := strfmt.DateTime(time.Now())
				host = hostutil.GenerateTestHost(hostId, clusterId, srcState)
				host.Inventory = hostutil.GenerateMasterInventory()
				host.Role = models.HostRoleMaster
				host.CheckedInAt = hostCheckInAt
				host.StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-passedTime))

				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				cluster = hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
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
					host = hostutil.GenerateTestHost(hostId, clusterId, srcState)
					host.Inventory = hostutil.GenerateMasterInventory()
					host.Role = models.HostRoleMaster
					host.CheckedInAt = hostCheckInAt

					progress := models.HostProgressInfo{
						CurrentStage:   stage,
						StageStartedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
						StageUpdatedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
					}

					host.Progress = &progress

					Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
					cluster = hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
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
						info := formatProgressTimedOutInfo(stage)
						Expect(swag.StringValue(resultHost.StatusInfo)).To(Equal(info))
					}

				})
			}
		}
		It("state info progress when failed", func() {

			cluster = hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

			masterID := strfmt.UUID("1")
			master := hostutil.GenerateTestHost(masterID, clusterId, models.HostStatusInstallingInProgress)
			master.Inventory = hostutil.GenerateMasterInventory()
			master.Role = models.HostRoleMaster
			master.CheckedInAt = strfmt.DateTime(time.Now())
			master.Progress = &models.HostProgressInfo{
				CurrentStage:   models.HostStageWaitingForControlPlane,
				StageStartedAt: strfmt.DateTime(time.Now().Add(-90 * time.Minute)),
				StageUpdatedAt: strfmt.DateTime(time.Now().Add(-90 * time.Minute)),
			}
			Expect(db.Create(&master).Error).ShouldNot(HaveOccurred())

			hostId = strfmt.UUID("2")
			host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusInstallingInProgress)
			host.Inventory = hostutil.GenerateMasterInventory()
			host.Role = models.HostRoleWorker
			host.CheckedInAt = strfmt.DateTime(time.Now())
			progress := models.HostProgressInfo{
				CurrentStage:   models.HostStageConfiguring,
				StageStartedAt: strfmt.DateTime(time.Now().Add(-90 * time.Minute)),
				StageUpdatedAt: strfmt.DateTime(time.Now().Add(-90 * time.Minute)),
			}
			host.Progress = &progress
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

			mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID,
				gomock.Any(), hostutil.GetEventSeverityFromHostStatus(models.HostStatusError), gomock.Any(), gomock.Any()).
				AnyTimes()

			err := hapi.RefreshStatus(ctx, &master, db)
			Expect(err).ToNot(HaveOccurred())

			err = hapi.RefreshStatus(ctx, &host, db)
			Expect(err).ToNot(HaveOccurred())

			var resultMaster models.Host
			Expect(db.Take(&resultMaster, "id = ? and cluster_id = ?", masterID.String(), clusterId.String()).Error).ToNot(HaveOccurred())

			var resultHost models.Host
			Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())

			info := formatProgressTimedOutInfo(models.HostStageWaitingForControlPlane)
			Expect(swag.StringValue(resultMaster.StatusInfo)).To(Equal(info))

			info = formatProgressTimedOutInfo(models.HostStageConfiguring)
			Expect(swag.StringValue(resultHost.StatusInfo)).To(Equal(info))
		})

	})

	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			// Test parameters
			name               string
			statusInfoChecker  statusInfoChecker
			validationsChecker *validationsChecker
			validCheckInTime   bool
			errorExpected      bool

			// Host fields
			srcState   string
			dstState   string
			inventory  string
			role       models.HostRole
			kind       string
			ntpSources []*models.NtpSource

			// Cluster fields
			machineNetworkCidr    string
			connectivity          string
			userManagedNetworking bool
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
					IsPlatformValid:      {status: ValidationPending, messagePattern: "Missing inventory"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
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
					IsPlatformValid:      {status: ValidationPending, messagePattern: "Missing inventory"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
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
					IsPlatformValid:      {status: ValidationPending, messagePattern: "Missing inventory"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
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
					IsPlatformValid:      {status: ValidationPending, messagePattern: "Missing inventory"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
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
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
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
					IsPlatformValid:      {status: ValidationPending, messagePattern: "Missing inventory"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
				}),
				errorExpected: false,
			},
			{
				name:             "disconnected to insufficient (1)",
				role:             models.HostRoleAutoAssign,
				validCheckInTime: true,
				srcState:         models.HostStatusDisconnected,
				dstState:         models.HostStatusInsufficient,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs",
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8 GiB, found only 0 GiB",
					"Require a disk of at least 120 GB",
					"Require at least 8 GiB RAM role auto-assign, found only 0",
					"Host couldn't synchronize with any NTP server")),
				inventory: insufficientHWInventory(),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8 GiB, found only 0 GiB"},
					HasMinValidDisks:     {status: ValidationFailure, messagePattern: "Require a disk of at least 120 GB"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role auto-assign"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 8 GiB RAM role auto-assign, found only 0"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
				}),
				errorExpected: false,
			},
			{
				name:             "insufficient to insufficient (1)",
				role:             models.HostRoleAutoAssign,
				validCheckInTime: true,
				srcState:         models.HostStatusInsufficient,
				dstState:         models.HostStatusInsufficient,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs",
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8 GiB, found only 0 GiB",
					"Require a disk of at least 120 GB",
					"Require at least 8 GiB RAM role auto-assign, found only 0",
					"Host couldn't synchronize with any NTP server")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8 GiB, found only 0 GiB"},
					HasMinValidDisks:     {status: ValidationFailure, messagePattern: "Require a disk of at least 120 GB"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role auto-assign"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 8 GiB RAM role auto-assign, found only 0"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
				}),
				inventory:     insufficientHWInventory(),
				errorExpected: false,
			},
			{
				name:             "discovering to insufficient (1)",
				role:             models.HostRoleAutoAssign,
				validCheckInTime: true,
				srcState:         models.HostStatusDiscovering,
				dstState:         models.HostStatusInsufficient,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs",
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8 GiB, found only 0 GiB",
					"Require a disk of at least 120 GB",
					"Require at least 8 GiB RAM role auto-assign, found only 0",
					"Host couldn't synchronize with any NTP server")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8 GiB, found only 0 GiB"},
					HasMinValidDisks:     {status: ValidationFailure, messagePattern: "Require a disk of at least 120 GB"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 8 GiB RAM role"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
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
				name:             "known to pending",
				validCheckInTime: true,
				srcState:         models.HostStatusKnown,
				dstState:         models.HostStatusPendingForInput,
				role:             models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoPendingForInput,
					"Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs",
					"Host couldn't synchronize with any NTP server")),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
				}),
				inventory:     workerInventory(),
				errorExpected: false,
			},
			{
				name:             "pending to pending",
				validCheckInTime: true,
				srcState:         models.HostStatusPendingForInput,
				dstState:         models.HostStatusPendingForInput,
				role:             models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoPendingForInput,
					"Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs",
					"Host couldn't synchronize with any NTP server")),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
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
				ntpSources:         []*models.NtpSource{common.TestNTPSourceUnsynced},
				role:               models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDR 5.6.7.0/24", "Host couldn't synchronize with any NTP server")),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
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
				ntpSources:         []*models.NtpSource{common.TestNTPSourceUnsynced},
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDR 5.6.7.0/24",
					"Require at least 4 CPU cores for master role, found only 2",
					"Require at least 16 GiB RAM role master, found only 8",
					"Host couldn't synchronize with any NTP server")),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
				}),
				inventory:     workerInventory(),
				errorExpected: false,
			},
			{
				name:               "discovering to insufficient (invalid system vendor)",
				validCheckInTime:   true,
				srcState:           models.HostStatusDiscovering,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Platform OpenStack Compute is forbidden")),
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
					IsPlatformValid:      {status: ValidationFailure, messagePattern: "Platform OpenStack Compute is forbidden"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:     inventoryWithUnauthorizedVendor(),
				errorExpected: false,
			},
			{
				name:               "insufficient to insufficient (2)",
				validCheckInTime:   true,
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Require at least 4 CPU cores for master role, found only 2", "Require at least 16 GiB RAM role master, found only 8")),
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
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
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
				ntpSources:         defaultNTPSources,
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Require at least 4 CPU cores for master role, found only 2", "Require at least 16 GiB RAM role master, found only 8")),
				inventory: workerInventory(),
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
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				errorExpected: false,
			},
			{
				name:               "known to insufficient (2)",
				validCheckInTime:   true,
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "5.6.7.0/24",
				ntpSources:         []*models.NtpSource{common.TestNTPSourceUnsynced},
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDR 5.6.7.0/24", "Host couldn't synchronize with any NTP server")),
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
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
				}),
				inventory:     hostutil.GenerateMasterInventory(),
				errorExpected: false,
			},
			{
				name:               "insufficient to insufficient (2)",
				validCheckInTime:   true,
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "5.6.7.0/24",
				ntpSources:         []*models.NtpSource{common.TestNTPSourceUnsynced},
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDR 5.6.7.0/24", "Host couldn't synchronize with any NTP server")),
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
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
				}),
				inventory:     hostutil.GenerateMasterInventory(),
				errorExpected: false,
			},
			{
				name:               "insufficient to insufficient (localhost)",
				validCheckInTime:   true,
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname localhost is forbidden")),
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
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:     hostutil.GenerateMasterInventoryWithHostname("localhost"),
				errorExpected: false,
			},
			{
				name:               "discovering to known",
				validCheckInTime:   true,
				srcState:           models.HostStatusDiscovering,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
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
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:     hostutil.GenerateMasterInventory(),
				errorExpected: false,
			},
			{
				name:               "discovering to known user managed networking",
				validCheckInTime:   true,
				srcState:           models.HostStatusDiscovering,
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
					IsMachineCidrDefined:   {status: ValidationSuccess, messagePattern: "No Machine Network CIDR needed: User Managed Networking"},
					HasCPUCoresForRole:     {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:       {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:       {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "No machine network CIDR validation needed: User Managed Networking"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsAPIVipConnected:      {status: ValidationSuccess, messagePattern: "No API VIP needed: User Managed Networking"},
					BelongsToMajorityGroup: {status: ValidationSuccess, messagePattern: "L2 connectivy validation skipped: User Managed Networking"},
				}),
				inventory:             hostutil.GenerateMasterInventory(),
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:               "insufficient to known",
				validCheckInTime:   true,
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
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
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:     hostutil.GenerateMasterInventory(),
				errorExpected: false,
			},
			{
				name:               "pending to known",
				validCheckInTime:   true,
				srcState:           models.HostStatusPendingForInput,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
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
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:     hostutil.GenerateMasterInventory(),
				errorExpected: false,
			},
			{
				name:               "pending to known IPv6",
				validCheckInTime:   true,
				srcState:           models.HostStatusPendingForInput,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1001:db8::/120",
				ntpSources:         defaultNTPSources,
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
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:     hostutil.GenerateMasterInventoryV6(),
				errorExpected: false,
			},
			{
				name:               "known to known",
				validCheckInTime:   true,
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
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
					IsNTPSynced:            {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:     hostutil.GenerateMasterInventory(),
				errorExpected: false,
			},
			{
				name:               "known to insufficient",
				validCheckInTime:   true,
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"No connectivity to the majority of hosts in the cluster")),
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
					IsNTPSynced:            {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:     hostutil.GenerateMasterInventory(),
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
				inventory:          hostutil.GenerateMasterInventory(),
				errorExpected:      true,
			},
			{
				name:              "AddedtoExistingCluster to AddedtoExistingCluster for day2 cloud",
				srcState:          models.HostStatusAddedToExistingCluster,
				dstState:          models.HostStatusAddedToExistingCluster,
				kind:              models.HostKindAddToExistingClusterHost,
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(""),
				errorExpected:     false,
			},
			{
				name:              "AddedtoExistingCluster to AddedtoExistingCluster for day2 OCP",
				srcState:          models.HostStatusAddedToExistingCluster,
				dstState:          models.HostStatusAddedToExistingCluster,
				kind:              models.HostKindAddToExistingClusterOCPHost,
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(""),
				errorExpected:     false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				// Test setup - Host creation
				hostCheckInAt := strfmt.DateTime(time.Now())
				if !t.validCheckInTime {
					// Timeout for checkin is 3 minutes so subtract 4 minutes from the current time
					hostCheckInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
				}
				srcState = t.srcState
				host = hostutil.GenerateTestHost(hostId, clusterId, srcState)
				host.Inventory = t.inventory
				host.Role = t.role
				host.CheckedInAt = hostCheckInAt
				host.Kind = swag.String(t.kind)
				bytes, err := json.Marshal(t.ntpSources)
				Expect(err).ShouldNot(HaveOccurred())
				host.NtpSources = string(bytes)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

				// Test setup - Cluster creation
				cluster = hostutil.GenerateTestCluster(clusterId, t.machineNetworkCidr)
				cluster.UserManagedNetworking = &t.userManagedNetworking
				if t.connectivity == "" {
					cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", t.machineNetworkCidr, hostId.String())
				} else {
					cluster.ConnectivityMajorityGroups = t.connectivity
				}
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				// Test definition
				if srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, hostutil.GetEventSeverityFromHostStatus(t.dstState),
						gomock.Any(), gomock.Any())
				}
				err = hapi.RefreshStatus(ctx, &host, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Role).To(Equal(t.role))
				Expect(resultHost.Status).To(Equal(&t.dstState))
				fmt.Println(swag.StringValue(resultHost.StatusInfo))
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
				host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusPreparingForInstallation)
				host.Inventory = hostutil.GenerateMasterInventory()
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				cluster = hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
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
		var ntpSources []*models.NtpSource

		BeforeEach(func() {
			otherHostID = strfmt.UUID(uuid.New().String())
			ntpSources = defaultNTPSources
		})

		tests := []struct {
			// Test parameters
			name               string
			statusInfoChecker  statusInfoChecker
			validationsChecker *validationsChecker
			errorExpected      bool

			// Host fields
			srcState          string
			dstState          string
			inventory         string
			role              models.HostRole
			requestedHostname string

			// Cluster fields
			machineNetworkCidr string

			// 2nd Host fields
			otherState             string
			otherRequestedHostname string
			otherInventory         string
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
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:      hostutil.GenerateMasterInventoryWithHostname("first"),
				otherState:     models.HostStatusInsufficient,
				otherInventory: hostutil.GenerateMasterInventoryWithHostname("second"),
				errorExpected:  false,
			},
			{
				name:               "insufficient to insufficient (same hostname) 1",
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname first is not unique in cluster")),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:      hostutil.GenerateMasterInventoryWithHostname("first"),
				otherState:     models.HostStatusInsufficient,
				otherInventory: hostutil.GenerateMasterInventoryWithHostname("first"),
				errorExpected:  false,
			},
			{
				name:               "insufficient to insufficient (same hostname) 2",
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname first is not unique in cluster")),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:              hostutil.GenerateMasterInventoryWithHostname("first"),
				otherState:             models.HostStatusInsufficient,
				otherInventory:         hostutil.GenerateMasterInventoryWithHostname("second"),
				otherRequestedHostname: "first",
				errorExpected:          false,
			},
			{
				name:               "insufficient to insufficient (same hostname) 3",
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname second is not unique in cluster")),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:         hostutil.GenerateMasterInventoryWithHostname("first"),
				requestedHostname: "second",
				otherState:        models.HostStatusInsufficient,
				otherInventory:    hostutil.GenerateMasterInventoryWithHostname("second"),
				errorExpected:     false,
			},
			{
				name:               "insufficient to insufficient (same hostname) 4 loveeee",
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname third is not unique in cluster")),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:              hostutil.GenerateMasterInventoryWithHostname("first"),
				requestedHostname:      "third",
				otherState:             models.HostStatusInsufficient,
				otherInventory:         hostutil.GenerateMasterInventoryWithHostname("second"),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:              hostutil.GenerateMasterInventoryWithHostname("first"),
				requestedHostname:      "third",
				otherState:             models.HostStatusInsufficient,
				otherInventory:         hostutil.GenerateMasterInventoryWithHostname("second"),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:      hostutil.GenerateMasterInventoryWithHostname("first"),
				otherState:     models.HostStatusInsufficient,
				otherInventory: hostutil.GenerateMasterInventoryWithHostname("second"),
				errorExpected:  false,
			},
			{
				name:               "known to insufficient (same hostname) 1",
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname first is not unique in cluster")),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:      hostutil.GenerateMasterInventoryWithHostname("first"),
				otherState:     models.HostStatusInsufficient,
				otherInventory: hostutil.GenerateMasterInventoryWithHostname("first"),
				errorExpected:  false,
			},
			{
				name:               "known to insufficient (same hostname) 2",
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname first is not unique in cluster")),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:              hostutil.GenerateMasterInventoryWithHostname("first"),
				otherState:             models.HostStatusInsufficient,
				otherInventory:         hostutil.GenerateMasterInventoryWithHostname("second"),
				otherRequestedHostname: "first",
				errorExpected:          false,
			},
			{
				name:               "known to insufficient (same hostname) 3",
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname second is not unique in cluster")),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:         hostutil.GenerateMasterInventoryWithHostname("first"),
				requestedHostname: "second",
				otherState:        models.HostStatusInsufficient,
				otherInventory:    hostutil.GenerateMasterInventoryWithHostname("second"),
				errorExpected:     false,
			},
			{
				name:               "known to insufficient (same hostname) 4",
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname third is not unique in cluster")),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:              hostutil.GenerateMasterInventoryWithHostname("first"),
				requestedHostname:      "third",
				otherState:             models.HostStatusInsufficient,
				otherInventory:         hostutil.GenerateMasterInventoryWithHostname("second"),
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
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:              hostutil.GenerateMasterInventoryWithHostname("first"),
				requestedHostname:      "third",
				otherState:             models.HostStatusInsufficient,
				otherInventory:         hostutil.GenerateMasterInventoryWithHostname("first"),
				otherRequestedHostname: "forth",
				errorExpected:          false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				// Test setup - Host creation
				srcState = t.srcState
				host = hostutil.GenerateTestHost(hostId, clusterId, srcState)
				host.Inventory = t.inventory
				host.Role = t.role
				host.CheckedInAt = strfmt.DateTime(time.Now())
				host.RequestedHostname = t.requestedHostname
				bytes, err := json.Marshal(ntpSources)
				Expect(err).ShouldNot(HaveOccurred())
				host.NtpSources = string(bytes)

				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

				// Test setup - 2nd Host creation
				otherHost := hostutil.GenerateTestHost(otherHostID, clusterId, t.otherState)
				otherHost.RequestedHostname = t.otherRequestedHostname
				otherHost.Inventory = t.otherInventory
				Expect(db.Create(&otherHost).Error).ShouldNot(HaveOccurred())

				// Test setup - Cluster creation
				cluster = hostutil.GenerateTestCluster(clusterId, t.machineNetworkCidr)
				cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", t.machineNetworkCidr, hostId.String())
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				// Test definition
				expectedSeverity := models.EventSeverityInfo
				if t.dstState == models.HostStatusInsufficient {
					expectedSeverity = models.EventSeverityWarning
				}
				if !t.errorExpected && srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, expectedSeverity,
						gomock.Any(), gomock.Any())
				}

				err = hapi.RefreshStatus(ctx, &host, db)
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
			models.HostStatusInstallingPendingUserAction,
			models.HostStatusResettingPendingUserAction,
		} {
			for _, installationStage := range []models.HostStage{
				models.HostStageStartingInstallation,
				models.HostStageWaitingForControlPlane,
				models.HostStageStartWaitingForControlPlane,
				models.HostStageInstalling,
				models.HostStageWritingImageToDisk,
				models.HostStageRebooting,
				models.HostStageWaitingForIgnition,
				models.HostStageConfiguring,
				models.HostStageJoined,
				models.HostStageDone,
				models.HostStageFailed,
			} {
				installationStage := installationStage
				srcState := srcState

				It(fmt.Sprintf("host src: %s cluster error: false", srcState), func() {
					h := hostutil.GenerateTestHost(hostId, clusterId, srcState)
					h.Progress.CurrentStage = installationStage
					h.Inventory = hostutil.GenerateMasterInventory()
					Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
					c := hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
					c.Status = swag.String(models.ClusterStatusInstalling)
					Expect(db.Create(&c).Error).ToNot(HaveOccurred())

					err := hapi.RefreshStatus(ctx, &h, db)

					Expect(err).ShouldNot(HaveOccurred())
					Expect(swag.StringValue(h.Status)).Should(Equal(srcState))
				})
				It(fmt.Sprintf("host src: %s cluster error: true", srcState), func() {
					h := hostutil.GenerateTestHost(hostId, clusterId, srcState)
					h.Progress.CurrentStage = installationStage
					h.Inventory = hostutil.GenerateMasterInventory()
					Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
					c := hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
					c.Status = swag.String(models.ClusterStatusError)
					Expect(db.Create(&c).Error).ToNot(HaveOccurred())

					mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, &hostId, models.EventSeverityError,
						fmt.Sprintf("Host master-hostname: updated status from \"%s\" to \"error\" (Host is part of a cluster that failed to install)", srcState),
						gomock.Any())

					err := hapi.RefreshStatus(ctx, &h, db)

					Expect(err).ShouldNot(HaveOccurred())
					Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusError))

					var resultHost models.Host
					Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
					Expect(swag.StringValue(resultHost.StatusInfo)).Should(Equal(statusInfoAbortingDueClusterErrors))
				})
			}
		}
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

func formatProgressTimedOutInfo(stage models.HostStage) string {
	timeFormat := InstallationProgressTimeout[stage].String()
	statusInfo := statusInfoInstallationInProgressTimedOut
	if stage == models.HostStageWritingImageToDisk {
		statusInfo = statusInfoInstallationInProgressWritingImageToDiskTimedOut
	}
	info := strings.Replace(statusInfo, "$STAGE", string(stage), 1)
	info = strings.Replace(info, "$MAX_TIME", timeFormat, 1)
	return info
}

func formatStatusInfoFailedValidation(statusInfo string, validationMessages ...string) string {
	sort.Strings(validationMessages)
	return strings.Replace(statusInfo, "$FAILING_VALIDATIONS", strings.Join(validationMessages, " ; "), 1)
}
