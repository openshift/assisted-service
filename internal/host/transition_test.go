package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/thoas/go-funk"
	"k8s.io/utils/pointer"
)

var (
	defaultMasterRequirements = models.ClusterHostRequirementsDetails{
		CPUCores:                         4,
		RAMMib:                           16384,
		DiskSizeGb:                       120,
		InstallationDiskSpeedThresholdMs: 10,
		NetworkLatencyThresholdMs:        pointer.Float64Ptr(100),
		PacketLossPercentage:             pointer.Float64Ptr(0),
	}
	defaultWorkerRequirements = models.ClusterHostRequirementsDetails{
		CPUCores:                         2,
		RAMMib:                           8192,
		DiskSizeGb:                       120,
		InstallationDiskSpeedThresholdMs: 10,
		NetworkLatencyThresholdMs:        pointer.Float64Ptr(1000),
		PacketLossPercentage:             pointer.Float64Ptr(10),
	}
	defaultSnoRequirements = models.ClusterHostRequirementsDetails{
		CPUCores:                         8,
		RAMMib:                           32768,
		DiskSizeGb:                       120,
		InstallationDiskSpeedThresholdMs: 10,
	}
)

func createValidatorCfg() *hardware.ValidatorCfg {
	return &hardware.ValidatorCfg{
		VersionedRequirements: hardware.VersionedRequirementsDecoder{
			"default": {
				Version:            "default",
				MasterRequirements: &defaultMasterRequirements,
				WorkerRequirements: &defaultWorkerRequirements,
				SNORequirements:    &defaultSnoRequirements,
			},
		},
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
		dbName            string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockEvents = events.NewMockHandler(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil, operatorsManager)
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
					"order to boot from disk test-serial (test-disk, /dev/disk/by-id/test-disk-id))",
				expectedStatusInfo: "Expected the host to boot from disk, but it booted the installation image - " +
					"please reboot and fix boot order to boot from disk test-serial (test-disk, /dev/disk/by-id/test-disk-id)",
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
					"order to boot from disk test-serial (test-disk, /dev/disk/by-id/test-disk-id))",
				expectedStatusInfo: "Expected the host to boot from disk, but it booted the installation image - " +
					"please reboot and fix boot order to boot from disk test-serial (test-disk, /dev/disk/by-id/test-disk-id)",
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
					InstallationDiskPath: common.TestDiskId,
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
		dbName            string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), mockMetric, defaultConfig, nil, operatorsManager)
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
		dbName            string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), mockMetric, defaultConfig, nil, operatorsManager)
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
		dbName            string
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		ctrl              *gomock.Controller
		mockEventsHandler *events.MockHandler
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEventsHandler = events.NewMockHandler(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil)
		hapi = NewManager(common.GetTestLog(), db, mockEventsHandler, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil, operatorsManager)
	})

	tests := []struct {
		state         string
		success       bool
		changeState   bool
		statusCode    int32
		expectedState string
	}{
		{state: models.HostStatusPreparingForInstallation, success: true, changeState: true, expectedState: models.HostStatusKnown},
		{state: models.HostStatusPreparingSuccessful, success: true, changeState: true, expectedState: models.HostStatusKnown},
		{state: models.HostStatusInstalling, success: true, changeState: true},
		{state: models.HostStatusInstallingInProgress, success: true, changeState: true},
		{state: models.HostStatusInstalled, success: true, changeState: true},
		{state: models.HostStatusError, success: true, changeState: true},
		{state: models.HostStatusDisabled, success: true, changeState: false},
		{state: models.HostStatusInstallingPendingUserAction, success: true, changeState: true},
		{state: models.HostStatusDiscovering, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusKnown, success: true, changeState: false},
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
			eventsNum := 1
			if t.changeState {
				eventsNum = 2
			}
			if t.success && t.state == models.HostStatusDisabled {
				eventsNum = 0
			}
			acceptNewEvents(eventsNum)
			err := hapi.CancelInstallation(ctx, &host, "reason", db)
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			if t.success {
				Expect(err).ShouldNot(HaveOccurred())

				expectedState := models.HostStatusCancelled
				if t.expectedState != "" {
					expectedState = t.expectedState
				}
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
		dbName            string
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		ctrl              *gomock.Controller
		mockEventsHandler *events.MockHandler
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEventsHandler = events.NewMockHandler(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil)
		hapi = NewManager(common.GetTestLog(), db, mockEventsHandler, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil, operatorsManager)
	})

	tests := []struct {
		state         string
		success       bool
		changeState   bool
		statusCode    int32
		expectedState string
	}{
		{state: models.HostStatusPreparingForInstallation, success: true, changeState: true, expectedState: models.HostStatusKnown},
		{state: models.HostStatusInstalling, success: true, changeState: true},
		{state: models.HostStatusInstallingInProgress, success: true, changeState: true},
		{state: models.HostStatusInstalled, success: true, changeState: true},
		{state: models.HostStatusError, success: true, changeState: true},
		{state: models.HostStatusDisabled, success: true, changeState: false},
		{state: models.HostStatusInstallingPendingUserAction, success: true, changeState: true},
		{state: models.HostStatusCancelled, success: true, changeState: true},
		{state: models.HostStatusAddedToExistingCluster, success: true, changeState: true},
		{state: models.HostStatusDiscovering, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusKnown, success: true, changeState: false},
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
			eventsNum := 1
			if t.changeState {
				eventsNum = 2
			}
			if t.success && t.state == models.HostStatusDisabled {
				eventsNum = 0
			}
			acceptNewEvents(eventsNum)
			err := hapi.ResetHost(ctx, &host, "reason", db)
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			if t.success {
				Expect(err).ShouldNot(HaveOccurred())

				expectedState := models.HostStatusResetting
				if t.expectedState != "" {
					expectedState = t.expectedState
				}
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
		cluster           common.Cluster
		mockHwValidator   *hardware.MockValidator
		dbName            string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil, operatorsManager)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("install host", func() {
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
				srcState:   models.HostStatusPreparingSuccessful,
				validation: failure,
			},
			{
				name:       "preparing",
				srcState:   models.HostStatusPreparingForInstallation,
				validation: failure,
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
			host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusPreparingSuccessful)
			host.StatusInfo = swag.String(statusInfoHostPreparationSuccessful)
			host.Inventory = hostutil.GenerateMasterInventory()
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			cluster = hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
			cluster.Status = swag.String(models.ClusterStatusInstalling)
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			clusterRequirements := models.ClusterHostRequirements{
				Total: &models.ClusterHostRequirementsDetails{
					CPUCores:   1,
					DiskSizeGb: 20,
					RAMMib:     2,
				},
			}
			mockHwValidator.EXPECT().GetClusterHostRequirements(gomock.Any(), gomock.Any(), gomock.Any()).Return(&clusterRequirements, nil)
			mockPreflightHardwareRequirements(mockHwValidator, &defaultMasterRequirements, &defaultWorkerRequirements)
			mockHwValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return([]*models.Disk{}).AnyTimes()
			mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("/dev/sda").AnyTimes()
		})

		It("success", func() {
			tx := db.Begin()
			Expect(tx.Error).To(BeNil())
			mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, models.EventSeverityInfo,
				fmt.Sprintf("Host %s: updated status from \"preparing-successful\" to \"installing\" (Installation is in progress)", "master-hostname"),
				gomock.Any())
			Expect(hapi.RefreshStatus(ctx, &host, tx)).ShouldNot(HaveOccurred())
			Expect(tx.Commit().Error).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusInstalling))
			Expect(*h.StatusInfo).Should(Equal(statusInfoInstalling))
		})

		It("rollback transition", func() {
			tx := db.Begin()
			Expect(tx.Error).To(BeNil())
			mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, models.EventSeverityInfo,
				fmt.Sprintf("Host %s: updated status from \"preparing-successful\" to \"installing\" (Installation is in progress)", "master-hostname"),
				gomock.Any())
			Expect(hapi.RefreshStatus(ctx, &host, tx)).ShouldNot(HaveOccurred())
			Expect(tx.Rollback().Error).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusPreparingSuccessful))
			Expect(*h.StatusInfo).Should(Equal(statusInfoHostPreparationSuccessful))
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
		dbName            string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil, operatorsManager)
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
		dbName            string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil, operatorsManager)
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
			Expect(h.NtpSources).ShouldNot(BeEmpty())
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

type regexChecker struct {
	pattern string
}

func (v *regexChecker) check(value *string) {
	if value == nil {
		Expect(v.pattern).To(Equal(""))
	} else {
		Expect(*value).To(MatchRegexp(v.pattern))
	}
}

func makeRegexChecker(pattern string) statusInfoChecker {
	return &regexChecker{pattern: pattern}
}

type validationsChecker struct {
	expected map[validationID]validationCheckResult
}

func (j *validationsChecker) check(validationsStr string) {
	validationRes := make(ValidationsStatus)
	Expect(json.Unmarshal([]byte(validationsStr), &validationRes)).ToNot(HaveOccurred())
	Expect(checkValidationInfoIsSorted(validationRes["operators"])).Should(BeTrue())
next:
	for id, checkedResult := range j.expected {
		category, err := id.category()
		Expect(err).ToNot(HaveOccurred())
		results, ok := validationRes[category]
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

func checkValidationInfoIsSorted(vRes ValidationResults) bool {
	return sort.SliceIsSorted(vRes, func(i, j int) bool {
		return vRes[i].ID < vRes[j].ID
	})
}

type validationCheckResult struct {
	status         ValidationStatus
	messagePattern string
}

func makeJsonChecker(expected map[validationID]validationCheckResult) *validationsChecker {
	return &validationsChecker{expected: expected}
}

var _ = Describe("Refresh Host", func() {
	const (
		minDiskSizeGb = 120
	)
	var (
		supportedGPU      = models.Gpu{VendorID: "10de", DeviceID: "1db6"}
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		cluster           common.Cluster
		mockEvents        *events.MockHandler
		ctrl              *gomock.Controller
		dbName            string
		mockHwValidator   *hardware.MockValidator
		validatorCfg      *hardware.ValidatorCfg
		operatorsManager  *operators.Manager
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		validatorCfg = createValidatorCfg()
		mockHwValidator.EXPECT().ListEligibleDisks(gomock.Any()).DoAndReturn(func(inventory *models.Inventory) []*models.Disk {
			// Mock the hwValidator behavior of performing simple filtering according to disk size, because these tests
			// rely on small disks to get filtered out.
			return funk.Filter(inventory.Disks, func(disk *models.Disk) bool {
				return disk.SizeBytes >= conversions.GibToBytes(minDiskSizeGb)
			}).([]*models.Disk)
		}).AnyTimes()
		mockHwValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(nil, nil).AnyTimes()
		operatorsOptions := operators.Options{
			CNVConfig: cnv.Config{SupportedGPUs: map[string]bool{
				fmt.Sprintf("%s:%s", supportedGPU.VendorID, supportedGPU.DeviceID): true,
			}},
		}
		operatorsManager = operators.NewManager(common.GetTestLog(), nil, operatorsOptions, nil)
		mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("/dev/sda").AnyTimes()
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, nil, defaultConfig, nil, operatorsManager)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("host installation timeout - cluster is pending user action", func() {

		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
		})

		tests := []struct {
			stage         models.HostStage
			expectTimeout bool
		}{
			{models.HostStageRebooting, false},
			{models.HostStageWaitingForControlPlane, false},
			{models.HostStageWaitingForController, false},
			{models.HostStageWaitingForBootkube, false},
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

		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
		})

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

		postRebootingStages := []models.HostStage{
			models.HostStageRebooting,
			models.HostStageWaitingForIgnition,
			models.HostStageConfiguring,
			models.HostStageJoined,
			models.HostStageDone,
		}

		for j := range postRebootingStages {
			stage := postRebootingStages[j]
			name := fmt.Sprintf("disconnection while host  %s", stage)
			It(name, func() {
				srcState = models.HostStatusInstalled
				host = hostutil.GenerateTestHost(hostId, clusterId, srcState)
				host.Inventory = hostutil.GenerateMasterInventory()
				host.Role = models.HostRoleWorker
				host.CheckedInAt = strfmt.DateTime(time.Now().Add(-MaxHostDisconnectionTime - time.Minute))

				progress := models.HostProgressInfo{
					CurrentStage:   models.HostStageDone,
					StageStartedAt: strfmt.DateTime(time.Now().Add(-90 * time.Minute)),
					StageUpdatedAt: strfmt.DateTime(time.Now().Add(-90 * time.Minute)),
				}

				host.Progress = &progress

				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				cluster = hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				err := hapi.RefreshStatus(ctx, &host, db)

				Expect(err).ToNot(HaveOccurred())
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())

				validationRes := make(ValidationsStatus)
				Expect(json.Unmarshal([]byte(resultHost.ValidationsInfo), &validationRes)).ToNot(HaveOccurred())
				for vCategory, vRes := range validationRes {
					fmt.Println(vCategory)
					for _, v := range vRes {
						Expect(v.ID).ToNot(Equal(IsConnected))
						Expect(v.ID).ToNot(Equal(IsNTPSynced))
					}
				}
			})
		}
	})

	Context("host disconnected & preparing for installation", func() {
		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
		})

		var srcState string

		passedTime := 90 * time.Minute
		It("host disconnected & preparing for installation", func() {
			srcState = models.HostStatusPreparingForInstallation
			host = hostutil.GenerateTestHost(hostId, clusterId, srcState)
			host.Inventory = hostutil.GenerateMasterInventory()
			host.Role = models.HostRoleMaster
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-MaxHostDisconnectionTime - time.Minute))

			progress := models.HostProgressInfo{
				StageStartedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
				StageUpdatedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
			}

			host.Progress = &progress

			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			cluster = hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

			mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, hostutil.GetEventSeverityFromHostStatus(models.HostStatusDisconnected),
				gomock.Any(), gomock.Any())
			err := hapi.RefreshStatus(ctx, &host, db)

			Expect(err).ToNot(HaveOccurred())
			var resultHost models.Host
			Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
			Expect(swag.StringValue(resultHost.Status)).To(Equal(models.HostStatusDisconnected))
			info := statusInfoConnectionTimedOut
			Expect(swag.StringValue(resultHost.StatusInfo)).To(MatchRegexp(info))
		})
	})

	Context("host installation timeout", func() {

		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
		})

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
		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
			mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("abc").AnyTimes()
		})

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

	Context("Validate host", func() {
		tests := []struct {
			// Test parameters
			name               string
			statusInfoMsg      string
			validationsChecker *validationsChecker

			// Host fields
			hostID           strfmt.UUID
			inventory        string
			role             models.HostRole
			dstState         string
			srcState         string
			hostRequirements *models.ClusterHostRequirementsDetails
		}{
			{
				name:          "insufficient worker memory",
				hostID:        strfmt.UUID("054e0100-f50e-4be7-874d-73861179e40d"),
				inventory:     hostutil.GenerateInventoryWithResourcesWithBytes(4, conversions.MibToBytes(150), conversions.MibToBytes(100), "worker"),
				role:          models.HostRoleWorker,
				srcState:      models.HostStatusDiscovering,
				dstState:      models.HostStatusInsufficient,
				statusInfoMsg: "Insufficient memory to deploy CNV. Required memory is 360 MiB but found 100 MiB",
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:            {status: ValidationSuccess, messagePattern: "Host is connected"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: ""},
					IsAPIVipConnected:      {status: ValidationSuccess, messagePattern: "API VIP connectivity success"},
					BelongsToMajorityGroup: {status: ValidationPending, messagePattern: "Machine Network CIDR or Connectivity Majority Groups missing"},
					HasInventory:           {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:         {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:           {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster bec"},
					HasMinValidDisks:       {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:   {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:     {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:       {status: ValidationFailure, messagePattern: "Require at least 8.35 GiB RAM for role worker, found only 100 MiB"},
					IsHostnameUnique:       {status: ValidationSuccess, messagePattern: "Hostname worker is unique in cluster"},
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR 1.2.3.0/24"},
					IsPlatformValid:        {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:            {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					AreLsoRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: ""},
					AreOcsRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: "ocs is disabled"},
					AreCnvRequirementsSatisfied:                    {status: ValidationFailure, messagePattern: "Insufficient memory to deploy CNV. Required memory is 360 MiB but found 100 MiB"},
				}),
				hostRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, RAMMib: 8550},
			},
			{
				name:          "insufficient master memory",
				hostID:        strfmt.UUID("054e0100-f50e-4be7-874d-73861179e40d"),
				inventory:     hostutil.GenerateInventoryWithResourcesWithBytes(8, conversions.MibToBytes(150), conversions.MibToBytes(100), "master"),
				role:          models.HostRoleMaster,
				srcState:      models.HostStatusDiscovering,
				dstState:      models.HostStatusInsufficient,
				statusInfoMsg: "Insufficient memory to deploy CNV. Required memory is 150 MiB but found 100 MiB",
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:            {status: ValidationSuccess, messagePattern: "Host is connected"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: ""},
					IsAPIVipConnected:      {status: ValidationSuccess, messagePattern: "API VIP connectivity success"},
					BelongsToMajorityGroup: {status: ValidationPending, messagePattern: "Machine Network CIDR or Connectivity Majority Groups missing"},
					HasInventory:           {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:         {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:           {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster bec"},
					HasMinValidDisks:       {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:   {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:     {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:       {status: ValidationFailure, messagePattern: "Require at least 16.15 GiB RAM for role master, found only 100 MiB"},
					IsHostnameUnique:       {status: ValidationSuccess, messagePattern: "Hostname master is unique in cluster"},
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR 1.2.3.0/24"},
					IsPlatformValid:        {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:            {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					AreLsoRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: ""},
					AreOcsRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: "ocs is disabled"},
					AreCnvRequirementsSatisfied:                    {status: ValidationFailure, messagePattern: "Insufficient memory to deploy CNV. Required memory is 150 MiB but found 100 MiB"},
				}),
				hostRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 8, RAMMib: 16537},
			},
			{
				name:          "insufficient worker cpu",
				hostID:        strfmt.UUID("054e0100-f50e-4be7-874d-73861179e40d"),
				inventory:     hostutil.GenerateInventoryWithResourcesWithBytes(1, conversions.GibToBytes(16), conversions.GibToBytes(16), "worker"),
				role:          models.HostRoleWorker,
				srcState:      models.HostStatusDiscovering,
				dstState:      models.HostStatusInsufficient,
				statusInfoMsg: "Insufficient CPU to deploy CNV. Required CPU count is 2 but found 1",
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:            {status: ValidationSuccess, messagePattern: "Host is connected"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: ""},
					IsAPIVipConnected:      {status: ValidationSuccess, messagePattern: "API VIP connectivity success"},
					BelongsToMajorityGroup: {status: ValidationPending, messagePattern: "Machine Network CIDR or Connectivity Majority Groups missing"},
					HasInventory:           {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:         {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required CPU cores for any role is 2, found only 1"},
					HasMinMemory:           {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:       {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:   {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:     {status: ValidationFailure, messagePattern: "Require at least 4 CPU cores for worker role, found only 1"},
					HasMemoryForRole:       {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:       {status: ValidationSuccess, messagePattern: "Hostname worker is unique in cluster"},
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR 1.2.3.0/24"},
					IsPlatformValid:        {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:            {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					AreLsoRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: ""},
					AreOcsRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: "ocs is disabled"},
					AreCnvRequirementsSatisfied:                    {status: ValidationFailure, messagePattern: "Insufficient CPU to deploy CNV. Required CPU count is 2 but found 1"},
				}),
				hostRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4},
			},
			{
				name:          "insufficient master cpu",
				hostID:        strfmt.UUID("054e0100-f50e-4be7-874d-73861179e40d"),
				inventory:     hostutil.GenerateInventoryWithResourcesWithBytes(1, conversions.GibToBytes(17), conversions.GibToBytes(17), "master"),
				role:          models.HostRoleMaster,
				srcState:      models.HostStatusDiscovering,
				dstState:      models.HostStatusInsufficient,
				statusInfoMsg: "Insufficient CPU to deploy CNV. Required CPU count is 4 but found 1",
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:            {status: ValidationSuccess, messagePattern: "Host is connected"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: ""},
					IsAPIVipConnected:      {status: ValidationSuccess, messagePattern: "API VIP connectivity success"},
					BelongsToMajorityGroup: {status: ValidationPending, messagePattern: "Machine Network CIDR or Connectivity Majority Groups missing"},
					HasInventory:           {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:         {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required CPU cores for any role is 2, found only 1"},
					HasMinMemory:           {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:       {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:   {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:     {status: ValidationFailure, messagePattern: "Require at least 8 CPU cores for master role, found only 1"},
					HasMemoryForRole:       {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:       {status: ValidationSuccess, messagePattern: "Hostname master is unique in cluster"},
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR 1.2.3.0/24"},
					IsPlatformValid:        {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:            {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					AreLsoRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: ""},
					AreOcsRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: "ocs is disabled"},
					AreCnvRequirementsSatisfied:                    {status: ValidationFailure, messagePattern: "Insufficient CPU to deploy CNV. Required CPU count is 4 but found 1"},
				}),
				hostRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 8},
			},
			{
				name:          "insufficient virtualization cpu",
				hostID:        strfmt.UUID("054e0100-f50e-4be7-874d-73861179e40d"),
				inventory:     hostutil.GenerateMasterInventoryWithHostnameAndCpuFlags("master", []string{"fpu", "vme", "de", "pse", "tsc", "msr"}),
				role:          models.HostRoleMaster,
				srcState:      models.HostStatusDiscovering,
				dstState:      models.HostStatusInsufficient,
				statusInfoMsg: "Hardware virtualization not supported",
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:            {status: ValidationSuccess, messagePattern: "Host is connected"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: ""},
					IsAPIVipConnected:      {status: ValidationSuccess, messagePattern: "API VIP connectivity success"},
					BelongsToMajorityGroup: {status: ValidationPending, messagePattern: "Machine Network CIDR or Connectivity Majority Groups missing"},
					HasInventory:           {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:         {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:           {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:       {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:   {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasMemoryForRole:       {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:       {status: ValidationSuccess, messagePattern: "Hostname master is unique in cluster"},
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR 1.2.3.0/24"},
					IsPlatformValid:        {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:            {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					AreLsoRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: ""},
					AreOcsRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: "ocs is disabled"},
					AreCnvRequirementsSatisfied:                    {status: ValidationFailure, messagePattern: "CPU does not have virtualization support"},
				}),
			},
			{
				name:          "insufficient worker memory for host with GPU",
				hostID:        strfmt.UUID("054e0100-f50e-4be7-874d-73861179e40d"),
				inventory:     hostutil.GenerateInventoryWithResources(4, 1, "worker", &supportedGPU),
				role:          models.HostRoleWorker,
				srcState:      models.HostStatusDiscovering,
				dstState:      models.HostStatusInsufficient,
				statusInfoMsg: "Insufficient memory to deploy CNV. Required memory is 1384 MiB but found 1024 MiB",
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:            {status: ValidationSuccess, messagePattern: "Host is connected"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: ""},
					IsAPIVipConnected:      {status: ValidationSuccess, messagePattern: "API VIP connectivity success"},
					BelongsToMajorityGroup: {status: ValidationPending, messagePattern: "Machine Network CIDR or Connectivity Majority Groups missing"},
					HasInventory:           {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:         {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:           {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster bec"},
					HasMinValidDisks:       {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:   {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:     {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:       {status: ValidationFailure, messagePattern: "Require at least 9.35 GiB RAM for role worker, found only 1.00 GiB"},
					IsHostnameUnique:       {status: ValidationSuccess, messagePattern: "Hostname worker is unique in cluster"},
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR 1.2.3.0/24"},
					IsPlatformValid:        {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:            {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					AreLsoRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: ""},
					AreOcsRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: "ocs is disabled"},
					AreCnvRequirementsSatisfied:                    {status: ValidationFailure, messagePattern: "Insufficient memory to deploy CNV. Required memory is 1384 MiB but found 1024 MiB"},
				}),
				hostRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, RAMMib: 9576},
			},
		}
		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				cluster = hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
				cluster.MonitoredOperators = []*models.MonitoredOperator{
					&lso.Operator,
					&cnv.Operator,
				}
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				host = hostutil.GenerateTestHost(t.hostID, clusterId, t.srcState)
				host.Inventory = t.inventory
				host.Role = t.role
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				if t.hostRequirements != nil {
					mockHwValidator.EXPECT().GetClusterHostRequirements(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(&models.ClusterHostRequirements{Total: t.hostRequirements}, nil)
					mockPreflightHardwareRequirements(mockHwValidator, &defaultMasterRequirements, &defaultWorkerRequirements)
				} else {
					mockDefaultClusterHostRequirements(mockHwValidator)
				}
				mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID,
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					AnyTimes()

				err := hapi.RefreshStatus(ctx, &host, db)
				Expect(err).ToNot(HaveOccurred())

				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", t.hostID, clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Role).To(Equal(t.role))
				Expect(resultHost.Status).To(Equal(&t.dstState))
				Expect(strings.Contains(*resultHost.StatusInfo, t.statusInfoMsg))
				if t.validationsChecker != nil {
					t.validationsChecker.check(resultHost.ValidationsInfo)
				}
			})
		}
	})

	createDiskInfo := func(path string, speedMs int64, exitCode int64) string {
		ret, _ := common.SetDiskSpeed(path, speedMs, exitCode, "")
		return ret
	}

	createSuccessfulImageStatuses := func() string {
		statuses, err := common.UnmarshalImageStatuses("")
		Expect(err).ToNot(HaveOccurred())
		common.SetImageStatus(statuses, &models.ContainerImageAvailability{
			Name:   "abc",
			Result: models.ContainerImageAvailabilityResultSuccess,
		})
		ret, err := common.MarshalImageStatuses(statuses)
		Expect(err).ToNot(HaveOccurred())
		return ret
	}
	createFailedImageStatuses := func() string {
		statuses, err := common.UnmarshalImageStatuses("")
		Expect(err).ToNot(HaveOccurred())
		common.SetImageStatus(statuses, &models.ContainerImageAvailability{
			Name:   "abc",
			Result: models.ContainerImageAvailabilityResultFailure,
		})
		ret, err := common.MarshalImageStatuses(statuses)
		Expect(err).ToNot(HaveOccurred())
		return ret
	}

	getHost := func(clusterID, hostID strfmt.UUID) *models.Host {
		var h models.Host
		Expect(db.First(&h, "id = ? and cluster_id = ?", hostID, clusterID).Error).ToNot(HaveOccurred())
		return &h
	}

	Context("Preparing for installation", func() {
		tests := []struct {
			// Test parameters
			name               string
			statusInfoChecker  statusInfoChecker
			validationsChecker *validationsChecker
			validCheckInTime   bool
			errorExpected      bool

			// Host fields
			dstState    string
			disksInfo   string
			imageStatus string

			// Cluster fields
			clusterState string
		}{
			{
				name:              "Preparing no change",
				validCheckInTime:  true,
				dstState:          models.HostStatusPreparingForInstallation,
				clusterState:      models.ClusterStatusPreparingForInstallation,
				statusInfoChecker: makeValueChecker(statusInfoPreparingForInstallation),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
			},
			{
				name:              "Cluster not in status",
				validCheckInTime:  true,
				dstState:          models.HostStatusKnown,
				clusterState:      models.ClusterStatusInsufficient,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
			},
			{
				name:              "Cluster not in status (2)",
				validCheckInTime:  true,
				dstState:          models.HostStatusKnown,
				clusterState:      models.ClusterStatusInsufficient,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk is sufficient"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				disksInfo: createDiskInfo("/dev/sda", 10, 0),
			},
			{
				name:              "Disk speed check failed",
				validCheckInTime:  true,
				dstState:          models.HostStatusInsufficient,
				clusterState:      models.ClusterStatusPreparingForInstallation,
				statusInfoChecker: makeRegexChecker("Host cannot be installed due to following failing validation.*While preparing the previous installation the installation disk speed measurement failed or was found to be insufficient"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationFailure, messagePattern: "While preparing the previous installation the installation disk speed measurement failed or was found to be insufficient"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				disksInfo: createDiskInfo("/dev/sda", 0, -1),
			},
			{
				name:              "Image pull failed",
				validCheckInTime:  true,
				dstState:          models.HostStatusInsufficient,
				clusterState:      models.ClusterStatusPreparingForInstallation,
				statusInfoChecker: makeRegexChecker("Host cannot be installed due to following failing validation.*Failed to fetch container images needed for installation from abc"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationFailure, messagePattern: "Failed to fetch container images needed for installation from"},
				}),
				imageStatus: createFailedImageStatuses(),
			},
			{
				name:              "Disk speed check succeeded",
				validCheckInTime:  true,
				dstState:          models.HostStatusPreparingForInstallation,
				clusterState:      models.ClusterStatusPreparingForInstallation,
				statusInfoChecker: makeValueChecker(statusInfoPreparingForInstallation),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk is sufficient"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				disksInfo: createDiskInfo("/dev/sda", 10, 0),
			},
			{
				name:              "All succeeded",
				validCheckInTime:  true,
				dstState:          models.HostStatusPreparingSuccessful,
				clusterState:      models.ClusterStatusPreparingForInstallation,
				statusInfoChecker: makeValueChecker(statusInfoHostPreparationSuccessful),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk is sufficient"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				disksInfo:   createDiskInfo("/dev/sda", 10, 0),
				imageStatus: createSuccessfulImageStatuses(),
			},
		}
		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				mockDefaultClusterHostRequirements(mockHwValidator)

				// Test setup - Host creation
				hostCheckInAt := strfmt.DateTime(time.Now())
				if !t.validCheckInTime {
					// Timeout for checkin is 3 minutes so subtract 4 minutes from the current time
					hostCheckInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
				}
				host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusPreparingForInstallation)
				host.StatusInfo = swag.String(statusInfoPreparingForInstallation)
				host.Inventory = hostutil.GenerateMasterInventoryWithHostname("master-0")
				host.CheckedInAt = hostCheckInAt
				host.DisksInfo = t.disksInfo
				host.ImagesStatus = t.imageStatus
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

				// Test setup - Cluster creation
				machineCidr := "1.2.3.0/24"
				cluster = hostutil.GenerateTestCluster(clusterId, machineCidr)
				cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", machineCidr, hostId.String())
				cluster.Status = swag.String(t.clusterState)
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				// Test definition
				if t.dstState != models.HostStatusPreparingForInstallation {
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, hostutil.GetEventSeverityFromHostStatus(t.dstState),
						gomock.Any(), gomock.Any())
				}
				Expect(getHost(clusterId, hostId).ValidationsInfo).To(BeEmpty())
				err := hapi.RefreshStatus(ctx, &host, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
					Expect(getHost(clusterId, hostId).ValidationsInfo).ToNot(BeEmpty())
				}
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(resultHost.StatusInfo)
				if t.validationsChecker != nil {
					t.validationsChecker.check(resultHost.ValidationsInfo)
				}
			})
		}
	})

	Context("Preparing successful", func() {
		tests := []struct {
			// Test parameters
			name              string
			statusInfoChecker statusInfoChecker
			validCheckInTime  bool
			errorExpected     bool

			// Host fields
			dstState  string
			disksInfo string

			// Cluster fields
			clusterState string
		}{
			{
				name:              "Preparing no change",
				validCheckInTime:  true,
				dstState:          models.HostStatusPreparingSuccessful,
				clusterState:      models.ClusterStatusPreparingForInstallation,
				statusInfoChecker: makeValueChecker(statusInfoHostPreparationSuccessful),
			},
			{
				name:              "Cluster not in status",
				validCheckInTime:  true,
				dstState:          models.HostStatusKnown,
				clusterState:      models.ClusterStatusInsufficient,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
			},
		}
		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				mockDefaultClusterHostRequirements(mockHwValidator)

				// Test setup - Host creation
				hostCheckInAt := strfmt.DateTime(time.Now())
				if !t.validCheckInTime {
					// Timeout for checkin is 3 minutes so subtract 4 minutes from the current time
					hostCheckInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
				}
				host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusPreparingSuccessful)
				host.StatusInfo = swag.String(statusInfoHostPreparationSuccessful)
				host.Inventory = hostutil.GenerateMasterInventoryWithHostname("master-0")
				host.CheckedInAt = hostCheckInAt
				host.DisksInfo = t.disksInfo
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

				// Test setup - Cluster creation
				machineCidr := "1.2.3.0/24"
				cluster = hostutil.GenerateTestCluster(clusterId, machineCidr)
				cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", machineCidr, hostId.String())
				cluster.Status = swag.String(t.clusterState)
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				// Test definition
				if t.dstState != models.HostStatusPreparingSuccessful {
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, hostutil.GetEventSeverityFromHostStatus(t.dstState),
						gomock.Any(), gomock.Any())
				}
				Expect(getHost(clusterId, hostId).ValidationsInfo).To(BeEmpty())
				err := hapi.RefreshStatus(ctx, &host, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
					Expect(getHost(clusterId, hostId).ValidationsInfo).ToNot(BeEmpty())
				}
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Status).To(Equal(&t.dstState))
				t.statusInfoChecker.check(resultHost.StatusInfo)
			})
		}
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
			srcState      string
			dstState      string
			inventory     string
			role          models.HostRole
			kind          string
			ntpSources    []*models.NtpSource
			imageStatuses map[string]*models.ContainerImageAvailability

			// Cluster fields
			machineNetworkCidr    string
			connectivity          string
			userManagedNetworking bool

			numAdditionalHosts int
			operators          []*models.MonitoredOperator
			hostRequirements   *models.ClusterHostRequirementsDetails
			disksInfo          string
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes",
					"Require a disk of at least 120 GB",
					"Require at least 8.00 GiB RAM for role auto-assign, found only 130 bytes",
					"Host couldn't synchronize with any NTP server")),
				inventory: insufficientHWInventory(),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes"},
					HasMinValidDisks:     {status: ValidationFailure, messagePattern: "Require a disk of at least 120 GB"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role auto-assign"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 8.00 GiB RAM for role auto-assign, found only 130 bytes"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes",
					"Require a disk of at least 120 GB",
					"Require at least 8.00 GiB RAM for role auto-assign, found only 130 bytes",
					"Host couldn't synchronize with any NTP server")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes"},
					HasMinValidDisks:     {status: ValidationFailure, messagePattern: "Require a disk of at least 120 GB"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role auto-assign"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 8.00 GiB RAM for role auto-assign, found only 130 bytes"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes",
					"Require a disk of at least 120 GB",
					"Require at least 8.00 GiB RAM for role auto-assign, found only 130 bytes",
					"Host couldn't synchronize with any NTP server")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes"},
					HasMinValidDisks:     {status: ValidationFailure, messagePattern: "Require a disk of at least 120 GB"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 8.00 GiB RAM for role auto-assign, found only 130 bytes"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
				name:             "known to insufficient (1)",
				role:             models.HostRoleAutoAssign,
				validCheckInTime: true,
				srcState:         models.HostStatusKnown,
				dstState:         models.HostStatusInsufficient,
				statusInfoChecker: makeValueChecker("Host does not meet the minimum hardware requirements: " +
					"Host couldn't synchronize with any NTP server ; Machine Network CIDR is undefined; the Machine " +
					"Network CIDR can be defined by setting either the API or Ingress virtual IPs ; Require a disk of at " +
					"least 120 GB ; Require at least 8.00 GiB RAM for role auto-assign, found only 130 bytes ; " +
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for " +
					"any role is 8.00 GiB, found only 130 bytes"),
				inventory:     insufficientHWInventory(),
				errorExpected: false,
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesFailure},
				role:               models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDR 5.6.7.0/24",
					"Host couldn't synchronize with any NTP server",
					"Failed to fetch container images needed for installation from image")),
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationFailure, messagePattern: "Failed to fetch container images needed for installation from image"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesFailure},
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDR 5.6.7.0/24",
					"Require at least 4 CPU cores for master role, found only 2",
					"Require at least 16.00 GiB RAM for role master, found only 8.00 GiB",
					"Host couldn't synchronize with any NTP server",
					"Failed to fetch container images needed for installation from image")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationFailure, messagePattern: "Require at least 4 CPU cores for master role, found only 2"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 16.00 GiB RAM for role master, found only 8.00 GiB"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationFailure, messagePattern: "Host does not belong to machine network CIDR "},
					IsPlatformValid:      {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationFailure, messagePattern: "Failed to fetch container images needed for installation from image"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Require at least 4 CPU cores for master role, found only 2", "Require at least 16.00 GiB RAM for role master, found only 8.00 GiB")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationFailure, messagePattern: "Require at least 4 CPU cores for master role, found only 2"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 16.00 GiB RAM for role master, found only 8.00 GiB"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Require at least 4 CPU cores for master role, found only 2", "Require at least 16.00 GiB RAM for role master, found only 8.00 GiB")),
				inventory: workerInventory(),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationFailure, messagePattern: "Require at least 4 CPU cores for master role, found only 2"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 16.00 GiB RAM for role master, found only 8.00 GiB"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesFailure},
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDR 5.6.7.0/24",
					"Host couldn't synchronize with any NTP server",
					"Failed to fetch container images needed for installation from image")),
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationFailure, messagePattern: "Failed to fetch container images needed for installation from image"},
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
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesFailure},
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDR 5.6.7.0/24",
					"Host couldn't synchronize with any NTP server",
					"Failed to fetch container images needed for installation from image")),
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationFailure, messagePattern: "Failed to fetch container images needed for installation from image"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
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
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:     hostutil.GenerateMasterInventory(),
				errorExpected: false,
			},
			{
				name:               "discovering to known user managed networking",
				validCheckInTime:   true,
				srcState:           models.HostStatusDiscovering,
				dstState:           models.HostStatusKnown,
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
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
					BelongsToMajorityGroup: {status: ValidationSuccess, messagePattern: "Host has connectivity to the majority of hosts in the cluster"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:             hostutil.GenerateMasterInventory(),
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:               "discovering to insufficient user managed networking",
				validCheckInTime:   true,
				srcState:           models.HostStatusDiscovering,
				dstState:           models.HostStatusInsufficient,
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeRegexChecker("Host cannot be installed due to following failing validation"),
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
					BelongsToMajorityGroup: {status: ValidationPending, messagePattern: "Not enough enabled hosts in cluster to calculate connectivity groups"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:             hostutil.GenerateMasterInventory(),
				errorExpected:         false,
				userManagedNetworking: true,
				connectivity:          fmt.Sprintf("{\"%s\":[]}", network.IPv4.String()),
			},
			{
				name:               "discovering to insufficient user managed networking - 3 hosts",
				validCheckInTime:   true,
				srcState:           models.HostStatusDiscovering,
				dstState:           models.HostStatusInsufficient,
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				machineNetworkCidr: "1.2.3.0/24",
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeRegexChecker("Host cannot be installed due to following failing validation"),
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
					BelongsToMajorityGroup: {status: ValidationFailure, messagePattern: "No connectivity to the majority of hosts in the cluster"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:             hostutil.GenerateMasterInventory(),
				errorExpected:         false,
				userManagedNetworking: true,
				connectivity:          fmt.Sprintf("{\"%s\":[]}", network.IPv4.String()),
				numAdditionalHosts:    2,
			},
			{
				name:               "insufficient to known",
				validCheckInTime:   true,
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:     hostutil.GenerateMasterInventory(),
				errorExpected: false,
			},
			{
				name:               "insufficient to insufficient (failed disk info)",
				validCheckInTime:   true,
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:               models.HostRoleWorker,
				statusInfoChecker:  makeRegexChecker("Host cannot be installed due to following failing validation"),
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationFailure, messagePattern: "While preparing the previous installation the installation disk speed measurement failed or was found to be insufficient"},
				}),
				inventory:     hostutil.GenerateMasterInventory(),
				disksInfo:     createDiskInfo("/dev/sda", 0, -1),
				errorExpected: false,
			},
			{
				name:               "insufficient to known (successful disk info)",
				validCheckInTime:   true,
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk is sufficient"},
				}),
				inventory:     hostutil.GenerateMasterInventory(),
				disksInfo:     createDiskInfo("/dev/sda", 10, 0),
				errorExpected: false,
			},
			{
				name:               "pending to known",
				validCheckInTime:   true,
				srcState:           models.HostStatusPendingForInput,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall)),
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
					BelongsToMajorityGroup: {status: ValidationPending, messagePattern: "Not enough enabled hosts in cluster to calculate connectivity groups"},
					IsNTPSynced:            {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:     hostutil.GenerateMasterInventory(),
				connectivity:  fmt.Sprintf("{\"%s\":[]}", "1.2.3.0/24"),
				errorExpected: false,
			},
			{
				name:               "known to insufficient + additional hosts",
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
				inventory:          hostutil.GenerateMasterInventory(),
				connectivity:       fmt.Sprintf("{\"%s\":[]}", "1.2.3.0/24"),
				errorExpected:      false,
				numAdditionalHosts: 2,
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
				name:               "CNV + LSO enabled: known to insufficient with 1 worker",
				validCheckInTime:   true,
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:               models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Require at least 4 CPU cores for worker role, found only 2")),
				inventory: hostutil.GenerateInventoryWithResources(2, 8, "worker-1"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationFailure, messagePattern: "Require at least 4 CPU cores for worker role, found only 2"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				errorExpected:      false,
				operators:          []*models.MonitoredOperator{&cnv.Operator, &lso.Operator},
				numAdditionalHosts: 0,
				hostRequirements:   &models.ClusterHostRequirementsDetails{CPUCores: 4, RAMMib: 8192},
			},
			{
				name:               "CNV + LSO enabled: insufficient to insufficient with 1 master",
				validCheckInTime:   true,
				srcState:           models.HostStatusInsufficient,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:               models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Insufficient CPU to deploy CNV. Required CPU count is 4 but found 1 ",
					"Require at least 16.15 GiB RAM for role master, found only 2.00 GiB",
					"Require at least 8 CPU cores for master role, found only 1",
					"The host is not eligible to participate in Openshift Cluster because the minimum required CPU cores for any role is 2, found only 1",
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 2.00 GiB")),
				inventory: hostutil.GenerateInventoryWithResources(1, 2, "master-1"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster "},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationFailure, messagePattern: "Require at least 8 CPU cores for master role, found only 1"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 16.15 GiB RAM for role master, found only 2.00 GiB"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				errorExpected:      false,
				operators:          []*models.MonitoredOperator{&cnv.Operator, &lso.Operator},
				numAdditionalHosts: 0,
				hostRequirements:   &models.ClusterHostRequirementsDetails{CPUCores: 8, RAMMib: 16537},
			},
			{
				name:               "CNV + LSO enabled: known to known with sufficient CPU and memory with 3 master",
				validCheckInTime:   true,
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeValueChecker(formatStatusInfoFailedValidation(statusInfoKnown)),
				inventory:          hostutil.GenerateInventoryWithResources(8, 17, "master-1"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname master-1 is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				errorExpected:      false,
				operators:          []*models.MonitoredOperator{&cnv.Operator, &lso.Operator},
				numAdditionalHosts: 2,
			},
			{
				name:               "CNV + OCS enabled: known to known with sufficient CPU and memory with 3 master",
				validCheckInTime:   true,
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:               models.HostRoleMaster,
				statusInfoChecker:  makeValueChecker(formatStatusInfoFailedValidation(statusInfoKnown)),
				inventory:          hostutil.GenerateInventoryWithResources(18, 64, "master-1"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname master-1 is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				errorExpected:      false,
				operators:          []*models.MonitoredOperator{&cnv.Operator, &lso.Operator},
				numAdditionalHosts: 2,
			},
			{
				name:               "CNV + OCS enabled: known to insufficient with lack of memory with 3 workers",
				validCheckInTime:   true,
				srcState:           models.HostStatusKnown,
				dstState:           models.HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				ntpSources:         defaultNTPSources,
				imageStatuses:      map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:               models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Require at least 8.35 GiB RAM for role worker, found only 8.00 GiB",
					"OCS unsupported Host Role for Compact Mode.")),
				inventory: hostutil.GenerateInventoryWithResourcesAndMultipleDisk(11, 8, "worker-1"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                 {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:              {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:            {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:        {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:          {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:            {status: ValidationFailure, messagePattern: "Require at least 8.35 GiB RAM for role worker, found only 8.00 GiB"},
					IsHostnameUnique:            {status: ValidationSuccess, messagePattern: "Hostname worker-1 is unique in cluster"},
					BelongsToMachineCidr:        {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsNTPSynced:                 {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					AreOcsRequirementsSatisfied: {status: ValidationFailure, messagePattern: "OCS unsupported Host Role for Compact Mode."},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				errorExpected:      false,
				operators:          []*models.MonitoredOperator{&cnv.Operator, &lso.Operator, &ocs.Operator},
				numAdditionalHosts: 2,
				hostRequirements:   &models.ClusterHostRequirementsDetails{CPUCores: 4, RAMMib: 8550},
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				if t.hostRequirements != nil {
					mockHwValidator.EXPECT().GetClusterHostRequirements(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(&models.ClusterHostRequirements{Total: t.hostRequirements}, nil)
					mockPreflightHardwareRequirements(mockHwValidator, &defaultMasterRequirements, &defaultWorkerRequirements)
				} else {
					mockDefaultClusterHostRequirements(mockHwValidator)
				}
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
				bytes, err = json.Marshal(t.imageStatuses)
				Expect(err).ShouldNot(HaveOccurred())
				host.ImagesStatus = string(bytes)
				host.DisksInfo = t.disksInfo
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

				for i := 0; i < t.numAdditionalHosts; i++ {
					id := strfmt.UUID(uuid.New().String())
					h := hostutil.GenerateTestHost(id, clusterId, srcState)
					h.Inventory = t.inventory
					h.Role = t.role
					h.CheckedInAt = hostCheckInAt
					h.Kind = swag.String(t.kind)
					h.RequestedHostname = fmt.Sprintf("additional-host-%d", i)
					bytes, err = json.Marshal(t.ntpSources)
					Expect(err).ShouldNot(HaveOccurred())
					h.NtpSources = string(bytes)
					Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
				}

				// Test setup - Cluster creation
				cluster = hostutil.GenerateTestCluster(clusterId, t.machineNetworkCidr)
				cluster.UserManagedNetworking = &t.userManagedNetworking
				if t.connectivity == "" {
					if t.userManagedNetworking {
						cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", network.IPv4.String(), hostId.String())
					} else {
						cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", t.machineNetworkCidr, hostId.String())
					}
				} else {
					cluster.ConnectivityMajorityGroups = t.connectivity
				}

				cluster.MonitoredOperators = t.operators
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				// Test definition
				if srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, &hostId, hostutil.GetEventSeverityFromHostStatus(t.dstState),
						gomock.Any(), gomock.Any())
				}
				Expect(getHost(clusterId, hostId).ValidationsInfo).To(BeEmpty())
				err = hapi.RefreshStatus(ctx, &host, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
					Expect(getHost(clusterId, hostId).ValidationsInfo).ToNot(BeEmpty())
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
		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
		})
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
				dstState:      models.HostStatusKnown,
				statusInfo:    statusInfoKnown,
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
		var (
			srcState      string
			otherHostID   strfmt.UUID
			ntpSources    []*models.NtpSource
			imageStatuses map[string]*models.ContainerImageAvailability
		)

		BeforeEach(func() {
			otherHostID = strfmt.UUID(uuid.New().String())
			ntpSources = defaultNTPSources
			imageStatuses = map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess}
			mockDefaultClusterHostRequirements(mockHwValidator)
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
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
				bytes, err = json.Marshal(imageStatuses)
				Expect(err).ShouldNot(HaveOccurred())
				host.ImagesStatus = string(bytes)

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
		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
			mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("abc").AnyTimes()
		})

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
				models.HostStageWaitingForController,
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
	Context("disabled host validations", func() {

		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
			defaultNTPSourcesInBytes, err := json.Marshal(defaultNTPSources)
			Expect(err).NotTo(HaveOccurred())
			cluster = hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusDiscovering)
			host.Inventory = hostutil.GenerateInventoryWithResourcesWithBytes(4, conversions.GibToBytes(16), conversions.GibToBytes(16), "master")
			host.Role = models.HostRoleMaster
			host.NtpSources = string(defaultNTPSourcesInBytes)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID,
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				AnyTimes()
		})

		tests := []struct {
			name                string
			disabledValidations DisabledHostValidations
			dstState            string
		}{
			{
				name:                "Nominal: Host is known with disabled validations for 'Belongs to majority group' and 'Container images available'",
				disabledValidations: DisabledHostValidations{string(models.HostValidationIDBelongsToMajorityGroup): struct{}{}, string(models.HostValidationIDContainerImagesAvailable): struct{}{}},
				dstState:            models.HostStatusKnown,
			},
			{
				name:                "KO: Host is insufficient with all validations enabled",
				disabledValidations: DisabledHostValidations{},
				dstState:            models.HostStatusInsufficient,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				defaultConfig.DisabledHostvalidations = t.disabledValidations
				hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, nil, defaultConfig, nil, operatorsManager)

				err := hapi.RefreshStatus(ctx, &host, db)
				Expect(err).ToNot(HaveOccurred())

				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId, clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Status).To(Equal(&t.dstState))
				validationRes := ValidationsStatus{}
				Expect(json.Unmarshal([]byte(resultHost.ValidationsInfo), &validationRes)).ToNot(HaveOccurred())
				for id := range t.disabledValidations {
					for _, cat := range validationRes {
						for _, val := range cat {
							if val.ID.String() == id {
								Expect(val.Status).To(Equal(ValidationDisabled))
								Expect(val.Message).To(Equal(validationDisabledByConfiguration))
							}
						}
					}
				}
			})
		}
	})
	Context("L3 network latency and packet loss validation", func() {

		defaultNTPSourcesInBytes, err := json.Marshal(defaultNTPSources)
		Expect(err).ShouldNot(HaveOccurred())
		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
			hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, nil, defaultConfig, nil, operatorsManager)
		})

		const (
			ipv4 = iota
			ipv6
		)
		tests := []struct {
			name                   string
			srcState               string
			dstState               string
			hostRole               models.HostRole
			latencyInMs            float64
			packetLossInPercentage float64
			statusInfoChecker      statusInfoChecker
			validationsChecker     *validationsChecker
			IPAddressPool          []string
			machineNetworkCIDR     string
			ipType                 int
		}{
			{name: "known with IPv4 and 3 masters",
				srcState:               models.HostStatusDiscovering,
				dstState:               models.HostStatusKnown,
				hostRole:               models.HostRoleMaster,
				latencyInMs:            50,
				packetLossInPercentage: 0,
				IPAddressPool:          hostutil.GenerateIPv4Addresses(2, "1.2.3.1/24"),
				machineNetworkCIDR:     "1.2.3.0/24",
				ipType:                 ipv4,
				statusInfoChecker:      makeValueChecker(formatStatusInfoFailedValidation(statusInfoKnown)),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasSufficientNetworkLatencyRequirementForRole: {status: ValidationSuccess, messagePattern: "Network latency requirement has been satisfied."},
					HasSufficientPacketLossRequirementForRole:     {status: ValidationSuccess, messagePattern: "Packet loss requirement has been satisfied."},
				}),
			}, {name: "known wit hIPv6 and 3 masters",
				srcState:               models.HostStatusDiscovering,
				dstState:               models.HostStatusKnown,
				hostRole:               models.HostRoleMaster,
				latencyInMs:            50,
				packetLossInPercentage: 0,
				IPAddressPool:          hostutil.GenerateIPv6Addresses(2, "1001:db8::1/120"),
				machineNetworkCIDR:     "1001:db8::/120",
				ipType:                 ipv4,
				statusInfoChecker:      makeValueChecker(formatStatusInfoFailedValidation(statusInfoKnown)),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasSufficientNetworkLatencyRequirementForRole: {status: ValidationSuccess, messagePattern: "Network latency requirement has been satisfied."},
					HasSufficientPacketLossRequirementForRole:     {status: ValidationSuccess, messagePattern: "Packet loss requirement has been satisfied."},
				}),
			}, {name: "known with IPv4 and 2 workers",
				srcState:               models.HostStatusDiscovering,
				dstState:               models.HostStatusKnown,
				hostRole:               models.HostRoleWorker,
				latencyInMs:            50,
				packetLossInPercentage: 0,
				IPAddressPool:          hostutil.GenerateIPv4Addresses(2, "1.2.3.1/24"),
				machineNetworkCIDR:     "1.2.3.0/24",
				ipType:                 ipv4,
				statusInfoChecker:      makeValueChecker(formatStatusInfoFailedValidation(statusInfoKnown)),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasSufficientNetworkLatencyRequirementForRole: {status: ValidationSuccess, messagePattern: "Network latency requirement has been satisfied."},
					HasSufficientPacketLossRequirementForRole:     {status: ValidationSuccess, messagePattern: "Packet loss requirement has been satisfied."},
				}),
			}, {name: "known with IPv6 and 2 workers",
				srcState:               models.HostStatusDiscovering,
				dstState:               models.HostStatusKnown,
				hostRole:               models.HostRoleWorker,
				latencyInMs:            50,
				packetLossInPercentage: 0,
				IPAddressPool:          hostutil.GenerateIPv6Addresses(2, "1001:db8::1/120"),
				machineNetworkCIDR:     "1001:db8::/120",
				ipType:                 ipv6,
				statusInfoChecker:      makeValueChecker(formatStatusInfoFailedValidation(statusInfoKnown)),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasSufficientNetworkLatencyRequirementForRole: {status: ValidationSuccess, messagePattern: "Network latency requirement has been satisfied."},
					HasSufficientPacketLossRequirementForRole:     {status: ValidationSuccess, messagePattern: "Packet loss requirement has been satisfied."},
				}),
			}, {name: "known with Single Node Openshift",
				srcState:               models.HostStatusDiscovering,
				dstState:               models.HostStatusKnown,
				hostRole:               models.HostRoleMaster,
				latencyInMs:            200,
				packetLossInPercentage: 50,
				IPAddressPool:          hostutil.GenerateIPv4Addresses(1, "1.2.3.1/24"),
				machineNetworkCIDR:     "1.2.3.0/24",
				ipType:                 ipv4,
				statusInfoChecker:      makeValueChecker(formatStatusInfoFailedValidation(statusInfoKnown)),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasSufficientNetworkLatencyRequirementForRole: {status: ValidationSuccess, messagePattern: "Network latency requirement has been satisfied."},
					HasSufficientPacketLossRequirementForRole:     {status: ValidationSuccess, messagePattern: "Packet loss requirement has been satisfied."},
				}),
			}, {name: "known with IPv4 and 3 nodes in autoassign",
				srcState:               models.HostStatusDiscovering,
				dstState:               models.HostStatusKnown,
				hostRole:               models.HostRoleAutoAssign,
				latencyInMs:            500,
				packetLossInPercentage: 5,
				IPAddressPool:          hostutil.GenerateIPv4Addresses(2, "1.2.3.1/24"),
				machineNetworkCIDR:     "1.2.3.0/24",
				ipType:                 ipv4,
				statusInfoChecker:      makeValueChecker(formatStatusInfoFailedValidation(statusInfoKnown)),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasSufficientNetworkLatencyRequirementForRole: {status: ValidationSuccess, messagePattern: "Network latency requirement has been satisfied."},
					HasSufficientPacketLossRequirementForRole:     {status: ValidationSuccess, messagePattern: "Packet loss requirement has been satisfied."},
				}),
			}, {name: "known with IPv4 and 3 masters with high latency and packet loss in autoassign",
				srcState:               models.HostStatusDiscovering,
				dstState:               models.HostStatusKnown,
				hostRole:               models.HostRoleAutoAssign,
				latencyInMs:            2000,
				packetLossInPercentage: 90,
				IPAddressPool:          hostutil.GenerateIPv4Addresses(3, "1.2.3.1/24"),
				machineNetworkCIDR:     "1.2.3.0/24",
				ipType:                 ipv4,
				statusInfoChecker:      makeValueChecker(formatStatusInfoFailedValidation(statusInfoKnown)),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasSufficientNetworkLatencyRequirementForRole: {status: ValidationSuccess, messagePattern: "Network latency requirement has been satisfied."},
					HasSufficientPacketLossRequirementForRole:     {status: ValidationSuccess, messagePattern: "Packet loss requirement has been satisfied."},
				}),
			}, {name: "insufficient with IPv4 and 3 masters with high latency and packet loss",
				srcState:               models.HostStatusDiscovering,
				dstState:               models.HostStatusInsufficient,
				hostRole:               models.HostRoleMaster,
				latencyInMs:            200,
				packetLossInPercentage: 1,
				IPAddressPool:          hostutil.GenerateIPv4Addresses(3, "1.2.3.1/24"),
				machineNetworkCIDR:     "1.2.3.0/24",
				ipType:                 ipv4,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Network latency requirements of less or equals than 100.000 ms not met for connectivity between master-0 and master-1,master-2.",
					"Packet loss percentage requirement of less or equals than 0.00% not met for connectivity between master-0 and master-1,master-2.")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasSufficientNetworkLatencyRequirementForRole: {status: ValidationFailure, messagePattern: "Network latency requirements of less or equals than 100.000 ms not met for connectivity between master-0 and master-1,master-2."},
					HasSufficientPacketLossRequirementForRole:     {status: ValidationFailure, messagePattern: "Packet loss percentage requirement of less or equals than 0.00% not met for connectivity between master-0 and master-1,master-2."},
				}),
			}, {name: "insufficient with IPv6 and 3 masters with high latency and packet loss",
				srcState:               models.HostStatusDiscovering,
				dstState:               models.HostStatusInsufficient,
				hostRole:               models.HostRoleMaster,
				latencyInMs:            200,
				packetLossInPercentage: 1,
				IPAddressPool:          hostutil.GenerateIPv6Addresses(3, "1001:db8::1/120"),
				machineNetworkCIDR:     "1001:db8::/120",
				ipType:                 ipv6,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Network latency requirements of less or equals than 100.000 ms not met for connectivity between master-0 and master-1,master-2.",
					"Packet loss percentage requirement of less or equals than 0.00% not met for connectivity between master-0 and master-1,master-2.")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasSufficientNetworkLatencyRequirementForRole: {status: ValidationFailure, messagePattern: "Network latency requirements of less or equals than 100.000 ms not met for connectivity between master-0 and master-1,master-2."},
					HasSufficientPacketLossRequirementForRole:     {status: ValidationFailure, messagePattern: "Packet loss percentage requirement of less or equals than 0.00% not met for connectivity between master-0 and master-1,master-2."},
				}),
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				hosts := []*models.Host{}
				for n := 0; n < len(t.IPAddressPool); n++ {
					netAddr := common.NetAddress{Hostname: fmt.Sprintf("%s-%d", t.hostRole, n)}
					if t.ipType == ipv4 {
						netAddr.IPv4Address = []string{t.IPAddressPool[n]}
					} else {
						netAddr.IPv6Address = []string{t.IPAddressPool[n]}
					}
					h := hostutil.GenerateTestHostWithNetworkAddress(strfmt.UUID(uuid.New().String()), clusterId, t.hostRole, t.srcState, netAddr)
					h.NtpSources = string(defaultNTPSourcesInBytes)
					hosts = append(hosts, h)
				}
				cluster = hostutil.GenerateTestCluster(clusterId, t.machineNetworkCIDR)
				cluster.UserManagedNetworking = swag.Bool(true)
				connectivityGroups := make(map[string][]strfmt.UUID)
				for _, h := range hosts {

					if network.IsIPV4CIDR(t.machineNetworkCIDR) {
						connectivityGroups[network.IPv4.String()] = append(connectivityGroups[network.IPv4.String()], *h.ID)
					} else {
						connectivityGroups[network.IPv6.String()] = append(connectivityGroups[network.IPv6.String()], *h.ID)
					}
				}
				b, err := json.Marshal(&connectivityGroups)
				Expect(err).ToNot(HaveOccurred())
				cluster.ConnectivityMajorityGroups = string(b)
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
				for n, h := range hosts {
					tmpHosts := []*models.Host{}
					tmpHosts = append(tmpHosts, hosts[:n]...)
					tmpHosts = append(tmpHosts, hosts[n+1:]...)
					rep := hostutil.GenerateL3ConnectivityReport(tmpHosts, t.latencyInMs, t.packetLossInPercentage)
					b, err := json.Marshal(&rep)
					h.Connectivity = string(b)
					Expect(db.Create(h).Error).ShouldNot(HaveOccurred())
					Expect(err).NotTo(HaveOccurred())
				}
				if t.srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
						gomock.Any(), gomock.Any())
				}
				Expect(hapi.RefreshStatus(ctx, hosts[0], db)).NotTo(HaveOccurred())

				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hosts[0].ID, clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Status).To(Equal(&t.dstState))
				fmt.Printf(*resultHost.StatusInfo)
				t.statusInfoChecker.check(resultHost.StatusInfo)
				if t.validationsChecker != nil {
					t.validationsChecker.check(resultHost.ValidationsInfo)
				}
			})
		}
	})
	Context("Default route", func() {

		ipv4Routes := []*models.Route{
			{Family: common.FamilyIPv4, Destination: "0.0.0.0", Gateway: "10.254.0.1"},
			{Family: common.FamilyIPv4, Destination: "192.168.122.0", Gateway: "0.0.0.0"}}
		ipv6Routes := []*models.Route{
			{Family: common.FamilyIPv6, Destination: net.IPv6zero.String(), Gateway: "2001:1::1"},
			{Family: common.FamilyIPv6, Destination: "2001:1::1", Gateway: net.IPv6zero.String()},
			{Family: common.FamilyIPv6, Destination: net.IPv6zero.String(), Gateway: net.IPv6zero.String()}}

		noDefaultRoute := []*models.Route{
			{Family: common.FamilyIPv4, Destination: "10.254.2.2", Gateway: "10.254.2.1"},
			{Family: common.FamilyIPv4, Destination: "172.17.0.15", Gateway: "172.17.0.1"},
			{Family: common.FamilyIPv6, Destination: "2001:1::10", Gateway: "2001:1::1"},
		}

		invalidDestination := []*models.Route{
			{Family: common.FamilyIPv4, Destination: "invalid", Gateway: "10.254.2.1"},
		}

		invalidGateway := []*models.Route{
			{Family: common.FamilyIPv4, Destination: "0.0.0.0", Gateway: "invalid"},
		}
		defaultNTPSourcesInBytes, err := json.Marshal(defaultNTPSources)
		Expect(err).ShouldNot(HaveOccurred())
		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
			hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, nil, defaultConfig, nil, operatorsManager)
			mockDefaultClusterHostRequirements(mockHwValidator)
			cluster = hostutil.GenerateTestCluster(clusterId, "1.2.3.0/24")
			cluster.UserManagedNetworking = swag.Bool(true)
			cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", network.IPv4.String(), hostId.String())
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

			mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID,
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				AnyTimes()

		})

		tests := []struct {
			name               string
			srcState           string
			dstState           string
			statusInfoChecker  statusInfoChecker
			validationsChecker *validationsChecker
			routes             []*models.Route
			inventory          string
		}{
			{name: "known with default route on IPv4 only",
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusKnown,
				routes:            ipv4Routes,
				inventory:         hostutil.GenerateMasterInventory(),
				statusInfoChecker: makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasDefaultRoute: {status: ValidationSuccess, messagePattern: "Host has been configured with at least one default route."},
				}),
			},
			{name: "known with default route on IPv6",
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusKnown,
				routes:            ipv6Routes,
				inventory:         hostutil.GenerateMasterInventory(),
				statusInfoChecker: makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasDefaultRoute: {status: ValidationSuccess, messagePattern: "Host has been configured with at least one default route."},
				}),
			},
			{name: "known with default route on IPv4 and IPv6",
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusKnown,
				routes:            append(ipv4Routes, ipv6Routes...),
				inventory:         hostutil.GenerateMasterInventory(),
				statusInfoChecker: makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasDefaultRoute: {status: ValidationSuccess, messagePattern: "Host has been configured with at least one default route."},
				}),
			},
			{name: "insufficient with no default route",
				srcState:  models.HostStatusDiscovering,
				dstState:  models.HostStatusInsufficient,
				routes:    noDefaultRoute,
				inventory: hostutil.GenerateMasterInventory(),
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host has not yet been configured with a default route.")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasDefaultRoute: {status: ValidationFailure, messagePattern: "Host has not yet been configured with a default route."},
				}),
			},
			{name: "insufficient with no routes",
				srcState:  models.HostStatusDiscovering,
				dstState:  models.HostStatusInsufficient,
				routes:    []*models.Route{},
				inventory: hostutil.GenerateMasterInventory(),
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host has not yet been configured with a default route.")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasDefaultRoute: {status: ValidationFailure, messagePattern: "Host has not yet been configured with a default route."},
				}),
			},
			{name: "discovering with pending inventory",
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusDiscovering,
				inventory:         "",
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoDiscovering)),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasDefaultRoute: {status: ValidationPending, messagePattern: "Missing default routing information."},
				}),
			},
			{name: "insufficient with no default route recorded",
				srcState:  models.HostStatusDiscovering,
				dstState:  models.HostStatusInsufficient,
				inventory: hostutil.GenerateMasterInventory(),
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host has not yet been configured with a default route.")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasDefaultRoute: {status: ValidationFailure, messagePattern: "Host has not yet been configured with a default route."},
				}),
			},
			{name: "insufficient with invalid destination",
				srcState:  models.HostStatusDiscovering,
				dstState:  models.HostStatusInsufficient,
				routes:    invalidDestination,
				inventory: hostutil.GenerateMasterInventory(),
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host has not yet been configured with a default route.")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasDefaultRoute: {status: ValidationFailure, messagePattern: "Host has not yet been configured with a default route."},
				}),
			},
			{name: "insufficient with invalid gateway",
				srcState:  models.HostStatusDiscovering,
				dstState:  models.HostStatusInsufficient,
				routes:    invalidGateway,
				inventory: hostutil.GenerateMasterInventory(),
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host has not yet been configured with a default route.")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasDefaultRoute: {status: ValidationFailure, messagePattern: "Host has not yet been configured with a default route."},
				}),
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				var inventory string
				if len(t.inventory) > 0 {
					inv, err := hostutil.UnmarshalInventory(t.inventory)
					Expect(err).To(Not(HaveOccurred()))
					inv.Routes = t.routes
					inventory, err = hostutil.MarshalInventory(inv)
					Expect(err).To(Not(HaveOccurred()))
				}
				host = hostutil.GenerateTestHost(hostId, clusterId, models.HostStatusDiscovering)
				host.Inventory = inventory
				host.NtpSources = string(defaultNTPSourcesInBytes)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				if t.srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
						gomock.Any(), gomock.Any())
				}
				Expect(hapi.RefreshStatus(ctx, &host, db)).NotTo(HaveOccurred())
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", host.ID, clusterId.String()).Error).ToNot(HaveOccurred())
				t.statusInfoChecker.check(resultHost.StatusInfo)
				Expect(resultHost.Status).To(Equal(&t.dstState))
				if t.validationsChecker != nil {
					t.validationsChecker.check(resultHost.ValidationsInfo)
				}
			})
		}
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})

var _ = Describe("validationResult sort", func() {
	It("validationResult sort", func() {
		validationResults := []ValidationResult{
			{ID: "cab", Status: "abc", Message: "abc"},
			{ID: "bac", Status: "abc", Message: "abc"},
			{ID: "acb", Status: "abc", Message: "abc"},
			{ID: "abc", Status: "abc", Message: "abc"},
		}
		sortByValidationResultID(validationResults)
		Expect(validationResults[0].ID.String()).Should(Equal("abc"))
		Expect(validationResults[1].ID.String()).Should(Equal("acb"))
		Expect(validationResults[2].ID.String()).Should(Equal("bac"))
		Expect(validationResults[3].ID.String()).Should(Equal("cab"))
	})
})

func mockDefaultClusterHostRequirements(mockHwValidator *hardware.MockValidator) {
	mockHwValidator.EXPECT().GetClusterHostRequirements(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirements, error) {
		var details models.ClusterHostRequirementsDetails
		if host.Role == models.HostRoleMaster {
			details = defaultMasterRequirements
		} else {
			details = defaultWorkerRequirements
		}
		return &models.ClusterHostRequirements{Total: &details}, nil
	})

	mockPreflightHardwareRequirements(mockHwValidator, &defaultMasterRequirements, &defaultWorkerRequirements)
}

func mockPreflightHardwareRequirements(mockHwValidator *hardware.MockValidator, masterRequirements, workerRequirements *models.ClusterHostRequirementsDetails) *gomock.Call {
	return mockHwValidator.EXPECT().GetPreflightHardwareRequirements(gomock.Any(), gomock.Any()).AnyTimes().Return(
		&models.PreflightHardwareRequirements{
			Ocp: &models.HostTypeHardwareRequirementsWrapper{
				Master: &models.HostTypeHardwareRequirements{
					Quantitative: masterRequirements,
				},
				Worker: &models.HostTypeHardwareRequirements{
					Quantitative: workerRequirements,
				},
			},
		}, nil)
}

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
