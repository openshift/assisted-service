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

	"github.com/filanov/stateswitch"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/feature"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/odf"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	"k8s.io/utils/pointer"
)

var (
	defaultMasterRequirements = models.ClusterHostRequirementsDetails{
		CPUCores:                         4,
		RAMMib:                           16384,
		DiskSizeGb:                       100,
		InstallationDiskSpeedThresholdMs: 10,
		NetworkLatencyThresholdMs:        pointer.Float64Ptr(100),
		PacketLossPercentage:             pointer.Float64Ptr(0),
	}
	defaultWorkerRequirements = models.ClusterHostRequirementsDetails{
		CPUCores:                         2,
		RAMMib:                           8192,
		DiskSizeGb:                       100,
		InstallationDiskSpeedThresholdMs: 10,
		NetworkLatencyThresholdMs:        pointer.Float64Ptr(1000),
		PacketLossPercentage:             pointer.Float64Ptr(10),
	}
	defaultSnoRequirements = models.ClusterHostRequirementsDetails{
		CPUCores:                         8,
		RAMMib:                           16384,
		DiskSizeGb:                       100,
		InstallationDiskSpeedThresholdMs: 10,
	}
)

var testAdditionalMachineCidr = []*models.MachineNetwork{{Cidr: "5.6.7.0/24"}}

var MaxHostDisconnectionTime = 3 * time.Minute

func createValidatorCfg() *hardware.ValidatorCfg {

	return &hardware.ValidatorCfg{
		Flags: feature.Flags{
			EnableUpgradeAgent: true,
		},
		VersionedRequirements: hardware.VersionedRequirementsDecoder{
			"default": {
				Version:                "default",
				MasterRequirements:     &defaultMasterRequirements,
				WorkerRequirements:     &defaultWorkerRequirements,
				SNORequirements:        &defaultSnoRequirements,
				EdgeWorkerRequirements: &defaultWorkerRequirements,
			},
		},
		MaximumAllowedTimeDiffMinutes: 4,
		MaxHostDisconnectionTime:      MaxHostDisconnectionTime,
		AgentDockerImage:              "quay.io/edge-infrastructure/assisted-installer-agent:latest",
	}
}

var _ = Describe("RegisterHost", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		ctrl                          *gomock.Controller
		mockEvents                    *eventsapi.MockHandler
		hostId, clusterId, infraEnvId strfmt.UUID
		dbName                        string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil, operatorsManager, nil, false, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("register_new", func() {
		Expect(hapi.RegisterHost(ctx, &models.Host{ID: &hostId, ClusterID: &clusterId, InfraEnvID: infraEnvId, DiscoveryAgentVersion: "v1.0.1"}, db)).ShouldNot(HaveOccurred())
		h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
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
					ID:         &hostId,
					InfraEnvID: infraEnvId,
					ClusterID:  &clusterId,
					Role:       models.HostRoleMaster,
					Inventory:  defaultHwInfo,
					Status:     swag.String(t.srcState),
					Progress: &models.HostProgressInfo{
						CurrentStage: t.progressStage,
					},
				}).Error).ShouldNot(HaveOccurred())

				if t.expectedEventInfo != "" && t.expectedEventStatus != "" {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(hostId.String()),
						eventstest.WithInfraEnvIdMatcher(infraEnvId.String()),
						eventstest.WithClusterIdMatcher(clusterId.String()),
						eventstest.WithSeverityMatcher(t.expectedEventStatus)))
				}

				err := hapi.RegisterHost(ctx, &models.Host{
					ID:         &hostId,
					ClusterID:  &clusterId,
					InfraEnvID: infraEnvId,
					Status:     swag.String(t.srcState),
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
				h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
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

	It("register host in error state", func() {
		Expect(db.Create(&models.Host{
			ID:         &hostId,
			InfraEnvID: infraEnvId,
			ClusterID:  &clusterId,
			Role:       models.HostRoleMaster,
			Inventory:  defaultHwInfo,
			Status:     swag.String(models.HostStatusError),
		}).Error).ShouldNot(HaveOccurred())

		Expect(hapi.RegisterHost(ctx, &models.Host{
			ID:                    &hostId,
			InfraEnvID:            infraEnvId,
			ClusterID:             &clusterId,
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
			kind     string
		}{
			{
				name:     "discovering",
				srcState: models.HostStatusDiscovering,
				kind:     models.HostKindHost,
			},
			{
				name:     "insufficient",
				srcState: models.HostStatusInsufficient,
				kind:     models.HostKindHost,
			},
			{
				name:     "disconnected",
				srcState: models.HostStatusDisconnected,
				kind:     models.HostKindHost,
			},
			{
				name:     "known",
				srcState: models.HostStatusKnown,
				kind:     models.HostKindHost,
			},
			{
				name:     "pending for input",
				srcState: models.HostStatusPendingForInput,
				kind:     models.HostKindHost,
			},
			{
				name:     "binding day1",
				srcState: models.HostStatusBinding,
				kind:     models.HostKindHost,
			},
			{
				name:     "binding day2",
				srcState: models.HostStatusBinding,
				kind:     models.HostKindAddToExistingClusterHost,
			},
		}

		AfterEach(func() {
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusDiscovering))
			Expect(h.Role).Should(Equal(models.HostRoleMaster))
			Expect(h.DiscoveryAgentVersion).To(Equal(discoveryAgentVersion))
			Expect(h.Bootstrap).To(BeFalse())

			// Verify resetted fields
			Expect(h.Inventory).To(BeEmpty())
			Expect(h.Progress.CurrentStage).To(BeEmpty())
			Expect(h.Progress.ProgressInfo).To(BeEmpty())
			Expect(h.NtpSources).To(BeEmpty())
			Expect(h.ImagesStatus).To(BeEmpty())
		})

		for i := range tests {
			t := tests[i]

			It(t.name, func() {
				Expect(db.Create(&models.Host{
					ID:           &hostId,
					InfraEnvID:   infraEnvId,
					ClusterID:    &clusterId,
					Role:         models.HostRoleMaster,
					Inventory:    defaultHwInfo,
					Status:       swag.String(t.srcState),
					Bootstrap:    true,
					ImagesStatus: "{'image': 'success'}",
					Kind:         swag.String(t.kind),
					Progress: &models.HostProgressInfo{
						CurrentStage: common.TestDefaultConfig.HostProgressStage,
						ProgressInfo: "some info",
					},
					NtpSources: "some ntp sources",
				}).Error).ShouldNot(HaveOccurred())
				if t.srcState != models.HostStatusDiscovering {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(hostId.String()),
						eventstest.WithInfraEnvIdMatcher(infraEnvId.String()),
						eventstest.WithClusterIdMatcher(clusterId.String()),
						eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
				}

				Expect(hapi.RegisterHost(ctx, &models.Host{
					ID:                    &hostId,
					InfraEnvID:            infraEnvId,
					ClusterID:             &clusterId,
					Status:                swag.String(t.srcState),
					Kind:                  swag.String(t.kind),
					DiscoveryAgentVersion: discoveryAgentVersion,
				},
					db)).ShouldNot(HaveOccurred())
				h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
				Expect(swag.StringValue(h.Kind)).To(Equal(t.kind))
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
					ID:         &hostId,
					InfraEnvID: infraEnvId,
					ClusterID:  &clusterId,
					Role:       models.HostRoleMaster,
					Inventory:  defaultHwInfo,
					Status:     swag.String(t.srcState),
				}).Error).ShouldNot(HaveOccurred())

				Expect(hapi.RegisterHost(ctx, &models.Host{
					ID:         &hostId,
					InfraEnvID: infraEnvId,
					ClusterID:  &clusterId,
					Status:     swag.String(t.srcState),
				},
					db)).Should(HaveOccurred())

				h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
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
					ClusterID:            &clusterId,
					InfraEnvID:           infraEnvId,
					Role:                 t.origRole,
					Inventory:            common.GenerateTestDefaultInventory(),
					Status:               swag.String(t.srcState),
					Progress:             &t.progress,
					InstallationDiskPath: common.TestDiskId,
					Kind:                 &t.hostKind,
				}).Error).ShouldNot(HaveOccurred())

				if t.eventSeverity != "" && t.eventMessage != "" {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(hostId.String()),
						eventstest.WithInfraEnvIdMatcher(infraEnvId.String()),
						eventstest.WithClusterIdMatcher(clusterId.String()),
						eventstest.WithSeverityMatcher(t.eventSeverity)))
				}

				Expect(hapi.RegisterHost(ctx, &models.Host{
					ID:         &hostId,
					InfraEnvID: infraEnvId,
					ClusterID:  &clusterId,
					Status:     swag.String(t.srcState),
				},
					db)).ShouldNot(HaveOccurred())

				h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
				Expect(swag.StringValue(h.Status)).Should(Equal(t.dstState))
				Expect(h.Role).Should(Equal(t.expectedRole))
				Expect(h.Inventory).Should(Equal(t.expectedInventory))
				Expect(swag.StringValue(h.StatusInfo)).Should(Equal(t.expectedStatusInfo))
			})
		}
	})

	Context("register unbound host", func() {
		var (
			infraEnvId strfmt.UUID
			clusterId  strfmt.UUID
			host       models.Host
		)

		BeforeEach(func() {
			infraEnvId = strfmt.UUID(uuid.New().String())
			clusterId = strfmt.UUID(uuid.New().String())
		})

		tests := []struct {
			name        string
			srcState    string
			dstState    string
			newHost     bool
			eventRaised bool
		}{
			{
				name:        "register new host to pool",
				srcState:    "",
				dstState:    models.HostStatusDiscoveringUnbound,
				newHost:     true,
				eventRaised: false,
			},
			{
				name:        "dicovering-unbound to discovering-unbound",
				srcState:    models.HostStatusDiscoveringUnbound,
				dstState:    models.HostStatusDiscoveringUnbound,
				newHost:     false,
				eventRaised: false,
			},
			{
				name:        "disconnected-unbound to discovering-unbound",
				srcState:    models.HostStatusDisconnectedUnbound,
				dstState:    models.HostStatusDiscoveringUnbound,
				newHost:     false,
				eventRaised: true,
			},
			{
				name:        "insufficient-unbound to discovering-unbound",
				srcState:    models.HostStatusInsufficientUnbound,
				dstState:    models.HostStatusDiscoveringUnbound,
				newHost:     false,
				eventRaised: true,
			},
			{
				name:        "known-unbound to discovering-unbound",
				srcState:    models.HostStatusKnownUnbound,
				dstState:    models.HostStatusDiscoveringUnbound,
				newHost:     false,
				eventRaised: true,
			},
			{
				name:        "unbinding to discovering-unbound",
				srcState:    models.HostStatusUnbinding,
				dstState:    models.HostStatusDiscoveringUnbound,
				newHost:     false,
				eventRaised: true,
			},
			{
				name:        "unbinding-pending-user-action to discovering-unbound",
				srcState:    models.HostStatusUnbindingPendingUserAction,
				dstState:    models.HostStatusDiscoveringUnbound,
				newHost:     false,
				eventRaised: true,
			},
			{
				name:        "reclaiming-rebooting to discovering-unbound",
				srcState:    models.HostStatusReclaimingRebooting,
				dstState:    models.HostStatusDiscoveringUnbound,
				newHost:     false,
				eventRaised: true,
			},
		}
		for i := range tests {
			t := tests[i]

			It(t.name, func() {
				if !t.newHost {
					host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, t.srcState)
					host.ClusterID = nil
					Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				}
				if t.eventRaised {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(hostId.String()),
						eventstest.WithInfraEnvIdMatcher(infraEnvId.String()),
						eventstest.WithClusterIdMatcher(swag.StringValue(nil)),
						eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(t.dstState))))
				}

				Expect(hapi.RegisterHost(ctx, &models.Host{
					ID:         &hostId,
					InfraEnvID: infraEnvId,
					ClusterID:  nil,
				}, db)).ShouldNot(HaveOccurred())

				h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
				Expect(swag.StringValue(h.Status)).Should(Equal(t.dstState))
			})
		}

	})
})

var _ = Describe("HostInstallationFailed", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		ctrl                          *gomock.Controller
		mockMetric                    *metrics.MockAPI
		mockEvents                    *eventsapi.MockHandler
		dbName                        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), mockMetric, defaultConfig, nil, operatorsManager, nil, false, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, "")
		host.Status = swag.String(models.HostStatusInstalling)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("handle_installation_error", func() {
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
			eventstest.WithHostIdMatcher(hostId.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvId.String()),
			eventstest.WithClusterIdMatcher(clusterId.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
		Expect(hapi.HandleInstallationFailure(ctx, &host)).ShouldNot(HaveOccurred())
		h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
		Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusError))
		Expect(swag.StringValue(h.StatusInfo)).Should(Equal("installation command failed"))
	})
})

var _ = Describe("Cancel host installation", func() {
	var (
		ctx                           = context.Background()
		dbName                        string
		hapi                          API
		db                            *gorm.DB
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		ctrl                          *gomock.Controller
		mockEventsHandler             *eventsapi.MockHandler
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEventsHandler = eventsapi.NewMockHandler(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		hapi = NewManager(common.GetTestLog(), db, mockEventsHandler, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil, operatorsManager, nil, false, nil)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
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
		{state: models.HostStatusInstallingPendingUserAction, success: true, changeState: true},
		{state: models.HostStatusDiscovering, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusKnown, success: true, changeState: false},
		{state: models.HostStatusPendingForInput, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusResettingPendingUserAction, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusDisconnected, success: false, statusCode: http.StatusConflict, changeState: false},
		{state: models.HostStatusCancelled, success: false, statusCode: http.StatusConflict, changeState: false},
	}

	acceptNewEvents := func(times int) {
		mockEventsHandler.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithHostIdMatcher(hostId.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvId.String()))).Times(times)
	}

	for _, t := range tests {
		t := t
		It(fmt.Sprintf("cancel from state %s", t.state), func() {
			hostId = strfmt.UUID(uuid.New().String())
			clusterId = strfmt.UUID(uuid.New().String())
			infraEnvId = strfmt.UUID(uuid.New().String())
			host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, "")
			host.Status = swag.String(t.state)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			eventsNum := 1
			if t.changeState {
				eventsNum = 2
			}
			acceptNewEvents(eventsNum)
			err := hapi.CancelInstallation(ctx, &host, "reason", db)
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
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
})

var _ = Describe("Install", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		ctrl                          *gomock.Controller
		mockEvents                    *eventsapi.MockHandler
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		cluster                       common.Cluster
		mockHwValidator               *hardware.MockValidator
		dbName                        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		pr := registry.NewMockProviderRegistry(ctrl)
		pr.EXPECT().IsHostSupported(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil, operatorsManager, pr, false, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("install host", func() {
		tests := []struct {
			name     string
			srcState string
		}{
			{
				name:     "prepared",
				srcState: models.HostStatusPreparingSuccessful,
			},
			{
				name:     "preparing",
				srcState: models.HostStatusPreparingForInstallation,
			},
			{
				name:     "known",
				srcState: models.HostStatusKnown,
			},
			{
				name:     "disconnected",
				srcState: models.HostStatusDisconnected,
			},
			{
				name:     "discovering",
				srcState: models.HostStatusDiscovering,
			},
			{
				name:     "error",
				srcState: models.HostStatusError,
			},
			{
				name:     "installed",
				srcState: models.HostStatusInstalled,
			},
			{
				name:     "installing",
				srcState: models.HostStatusInstalling,
			},
			{
				name:     "in-progress",
				srcState: models.HostStatusInstallingInProgress,
			},
			{
				name:     "insufficient",
				srcState: models.HostStatusInsufficient,
			},
			{
				name:     "resetting",
				srcState: models.HostStatusResetting,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, t.srcState)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				Expect(hapi.Install(ctx, &host, nil)).To(HaveOccurred())
			})
		}
	})

	Context("install with transaction", func() {
		BeforeEach(func() {
			host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusPreparingSuccessful)
			host.StatusInfo = swag.String(statusInfoHostPreparationSuccessful)
			host.Inventory = hostutil.GenerateMasterInventory()
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			cluster = hostutil.GenerateTestCluster(clusterId)
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
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(host.ID.String()),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String()),
			))
			Expect(hapi.RefreshStatus(ctx, &host, tx)).ShouldNot(HaveOccurred())
			Expect(tx.Commit().Error).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusInstalling))
			Expect(*h.StatusInfo).Should(Equal(statusInfoInstalling))
		})

		It("rollback transition", func() {
			tx := db.Begin()
			Expect(tx.Error).To(BeNil())
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(host.ID.String()),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
			Expect(hapi.RefreshStatus(ctx, &host, tx)).ShouldNot(HaveOccurred())
			Expect(tx.Rollback().Error).ShouldNot(HaveOccurred())
			h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusPreparingSuccessful))
			Expect(*h.StatusInfo).Should(Equal(statusInfoHostPreparationSuccessful))
		})
	})
})

var _ = Describe("Unbind", func() {
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
		mockHwValidator := hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), nil, defaultConfig, nil, operatorsManager, nil, false, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	success := func(reply error, dstState string) {
		Expect(reply).To(BeNil())
		h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
		Expect(h.ClusterID).Should(BeNil())
		Expect(*h.Status).Should(Equal(dstState))
		Expect(*h.StatusInfo).Should(Equal(statusInfoUnbinding))
		Expect(*h.Kind).Should(Equal(models.HostKindHost))
		Expect(h.Inventory).Should(BeEmpty())
		Expect(h.Bootstrap).Should(BeFalse())
		Expect(h.NtpSources).ShouldNot(BeEmpty())
		Expect(h.Connectivity).Should(BeEmpty())
		Expect(h.APIVipConnectivity).Should(BeEmpty())
		Expect(h.DomainNameResolutions).Should(BeEmpty())
		Expect(h.FreeAddresses).Should(BeEmpty())
		Expect(h.ImagesStatus).Should(BeEmpty())
		Expect(h.InstallationDiskID).Should(BeEmpty())
		Expect(h.InstallationDiskPath).Should(BeEmpty())
		Expect(h.MachineConfigPoolName).Should(BeEmpty())
		Expect(h.Role).Should(Equal(models.HostRoleAutoAssign))
		Expect(h.SuggestedRole).Should(BeEmpty())
		bytes, err := json.Marshal(defaultNTPSources)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(h.NtpSources).Should(Equal(string(bytes)))
		resetPogress := &models.HostProgressInfo{
			CurrentStage:           "",
			InstallationPercentage: 0,
			ProgressInfo:           "",
			StageStartedAt:         strfmt.DateTime(time.Time{}),
			StageUpdatedAt:         strfmt.DateTime(time.Time{}),
		}
		validateEqualProgress(h.Progress, resetPogress)
		Expect(h.ProgressStages).Should(BeNil())
		Expect(h.LogsInfo).Should(BeEmpty())
		Expect(time.Time(h.LogsStartedAt).Equal(time.Time{})).Should(BeTrue())
		Expect(time.Time(h.LogsCollectedAt).Equal(time.Time{})).Should(BeTrue())
		Expect(time.Time(h.StageStartedAt).Equal(time.Time{})).Should(BeTrue())
		Expect(time.Time(h.StageUpdatedAt).Equal(time.Time{})).Should(BeTrue())
	}

	failure := func(reply error, srcState string) {
		Expect(reply).Should(HaveOccurred())
		h := hostutil.GetHostFromDB(hostId, infraEnvId, db)
		Expect(*h.Status).Should(Equal(srcState))
		Expect(h.Inventory).Should(Equal(defaultHwInfo))
		Expect(h.Bootstrap).Should(Equal(true))
		var ntpSources []*models.NtpSource
		Expect(json.Unmarshal([]byte(h.NtpSources), &ntpSources)).ShouldNot(HaveOccurred())
		Expect(ntpSources).Should(Equal(defaultNTPSources))
	}

	tests := []struct {
		name      string
		srcState  string
		dstState  string
		success   bool
		sendEvent bool
		reclaim   bool
	}{
		{
			name:      "known to unbinding",
			srcState:  models.HostStatusKnown,
			dstState:  models.HostStatusUnbinding,
			success:   true,
			sendEvent: true,
			reclaim:   false,
		},
		{
			name:      "known to unbinding - reclaim",
			srcState:  models.HostStatusKnown,
			dstState:  models.HostStatusUnbinding,
			success:   true,
			sendEvent: true,
			reclaim:   true,
		},
		{
			name:      "disconnected to unbinding",
			srcState:  models.HostStatusDisconnected,
			dstState:  models.HostStatusUnbinding,
			success:   true,
			sendEvent: true,
			reclaim:   false,
		},
		{
			name:      "disconnected to unbinding - reclaim",
			srcState:  models.HostStatusDisconnected,
			dstState:  models.HostStatusUnbinding,
			success:   true,
			sendEvent: true,
			reclaim:   true,
		},
		{
			name:      "discovering to unbinding",
			srcState:  models.HostStatusDiscovering,
			dstState:  models.HostStatusUnbinding,
			success:   true,
			sendEvent: true,
			reclaim:   false,
		},
		{
			name:      "discovering to unbinding - reclaim",
			srcState:  models.HostStatusDiscovering,
			dstState:  models.HostStatusUnbinding,
			success:   true,
			sendEvent: true,
			reclaim:   true,
		},
		{
			name:      "pending-for-input to binding",
			srcState:  models.HostStatusPendingForInput,
			dstState:  models.HostStatusUnbinding,
			success:   true,
			sendEvent: true,
			reclaim:   false,
		},
		{
			name:      "pending-for-input to binding - reclaim",
			srcState:  models.HostStatusPendingForInput,
			dstState:  models.HostStatusUnbinding,
			success:   true,
			sendEvent: true,
			reclaim:   true,
		},
		{
			name:      "error to unbinding-pending-user-action",
			srcState:  models.HostStatusError,
			dstState:  models.HostStatusUnbindingPendingUserAction,
			success:   true,
			sendEvent: true,
			reclaim:   false,
		},
		{
			name:      "error to unbinding-pending-user-action - reclaim",
			srcState:  models.HostStatusError,
			dstState:  models.HostStatusUnbindingPendingUserAction,
			success:   true,
			sendEvent: true,
			reclaim:   true,
		},
		{
			name:      "cancelled to unbinding-pending-user-action",
			srcState:  models.HostStatusCancelled,
			dstState:  models.HostStatusUnbindingPendingUserAction,
			success:   true,
			sendEvent: true,
			reclaim:   false,
		},
		{
			name:      "cancelled to unbinding-pending-user-action - reclaim",
			srcState:  models.HostStatusCancelled,
			dstState:  models.HostStatusUnbindingPendingUserAction,
			success:   true,
			sendEvent: true,
			reclaim:   true,
		},
		{
			name:      "installing noop",
			srcState:  models.HostStatusInstalling,
			dstState:  models.HostStatusInstalling,
			success:   false,
			sendEvent: false,
			reclaim:   false,
		},
		{
			name:      "installing noop - reclaim",
			srcState:  models.HostStatusInstalling,
			dstState:  models.HostStatusInstalling,
			success:   false,
			sendEvent: false,
			reclaim:   true,
		},
		{
			name:      "added-host-to-existing-cluster to unbinding-pending-user-action",
			srcState:  models.HostStatusAddedToExistingCluster,
			dstState:  models.HostStatusUnbindingPendingUserAction,
			success:   true,
			sendEvent: true,
			reclaim:   false,
		},
		{
			name:      "added-host-to-existing-cluster to reclaiming",
			srcState:  models.HostStatusAddedToExistingCluster,
			dstState:  models.HostStatusReclaiming,
			success:   true,
			sendEvent: true,
			reclaim:   true,
		},
		{
			name:      "installed to reclaiming",
			srcState:  models.HostStatusInstalled,
			dstState:  models.HostStatusReclaiming,
			success:   true,
			sendEvent: true,
			reclaim:   true,
		},
	}
	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			// Test setup - Host creation
			host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, t.srcState)
			host.Inventory = defaultHwInfo
			host.Bootstrap = true
			host.APIVipConnectivity = "whatever"
			host.Connectivity = "whatever"
			host.DisksInfo = "whatever"
			host.DomainNameResolutions = "whatever"
			host.FreeAddresses = "whatever"
			host.ImagesStatus = "whatever"
			host.InstallationDiskID = "whatever"
			host.InstallationDiskPath = "whatever"
			host.MachineConfigPoolName = "whatever"
			host.ValidationsInfo = "whatever"
			host.SuggestedRole = models.HostRoleBootstrap
			host.Role = models.HostRoleMaster
			host.Progress = &models.HostProgressInfo{
				CurrentStage:           models.HostStageJoined,
				InstallationPercentage: 60,
				ProgressInfo:           "whatever",
				StageStartedAt:         strfmt.DateTime(time.Now()),
				StageUpdatedAt:         strfmt.DateTime(time.Now()),
			}
			host.ProgressStages = MasterStages[:]
			host.LogsInfo = models.LogsStateRequested
			host.LogsStartedAt = strfmt.DateTime(time.Now())
			host.LogsCollectedAt = strfmt.DateTime(time.Now())
			host.StageStartedAt = strfmt.DateTime(time.Now())
			host.StageUpdatedAt = strfmt.DateTime(time.Now())

			dstState := t.dstState

			bytes, err := json.Marshal(defaultNTPSources)
			Expect(err).ShouldNot(HaveOccurred())
			host.NtpSources = string(bytes)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

			// Test definition
			if t.sendEvent {
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), gomock.Any())
			}
			validation := success
			validationState := dstState
			if !t.success {
				validation = failure
				validationState = t.srcState
			}
			validation(hapi.UnbindHost(ctx, &host, db, t.reclaim), validationState)
		})
	}
})

type statusInfoChecker interface {
	check(statusInfo *string)
	String() string
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

func (v *valueChecker) String() string {
	return v.value
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

func (v *regexChecker) String() string {
	return v.pattern
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
		minDiskSizeGb = 100
	)
	var (
		supportedGPU                  = models.Gpu{VendorID: "10de", DeviceID: "1db6"}
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		hostId, clusterId, infraEnvId strfmt.UUID
		host                          models.Host
		cluster                       common.Cluster
		mockEvents                    *eventsapi.MockHandler
		ctrl                          *gomock.Controller
		dbName                        string
		mockHwValidator               *hardware.MockValidator
		validatorCfg                  *hardware.ValidatorCfg
		operatorsManager              *operators.Manager
		pr                            *registry.MockProviderRegistry
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
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
		operatorsManager = operators.NewManager(common.GetTestLog(), nil, operatorsOptions, nil, nil)
		mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("/dev/sda").AnyTimes()
		pr = registry.NewMockProviderRegistry(ctrl)
		pr.EXPECT().IsHostSupported(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, nil, defaultConfig, nil, operatorsManager, pr, false, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
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
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusInstallingInProgress)
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
				cluster = hostutil.GenerateTestCluster(clusterId)
				cluster.Status = swag.String(models.ClusterStatusInstallingPendingUserAction)
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				if t.expectTimeout {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(host.ID.String()),
						eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
						eventstest.WithClusterIdMatcher(host.ClusterID.String()),
						eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(models.HostStatusError))))
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
					Expect(swag.StringValue(resultHost.StatusInfo)).Should(Equal(formatProgressTimedOutInfo(defaultConfig, t.stage)))
				} else {
					if funk.Contains(WrongBootOrderIgnoreTimeoutStages, t.stage) {
						Expect(prevStageUpdatedAt).ShouldNot(Equal(currStageUpdateAt))
					}
					Expect(swag.StringValue(resultHost.Status)).Should(Equal(models.HostStatusInstallingInProgress))
				}
			})
		}

		Context("Integrate with vsphere", func() {
			createCluster := func() {
				cluster = hostutil.GenerateTestCluster(clusterId)
				cluster.Name = common.TestDefaultConfig.ClusterName
				cluster.BaseDNSDomain = common.TestDefaultConfig.BaseDNSDomain
				cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", common.TestIPv4Networking.MachineNetworks[0].Cidr, hostId.String())
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			}

			createHost := func(hostStatus string) {
				host = hostutil.GenerateTestHostByKind(hostId, infraEnvId, &clusterId, hostStatus, models.HostKindHost, models.HostRoleMaster)
				host.Inventory = hostutil.GenerateMasterInventory()
				bytes, err := json.Marshal(common.TestDomainNameResolutionsSuccess)
				Expect(err).ShouldNot(HaveOccurred())
				host.DomainNameResolutions = string(bytes)
				host.Timestamp = time.Now().Unix()
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
			}

			updateClusterPlatform := func(platformType models.PlatformType) {
				cluster.Platform = &models.Platform{
					Type: common.PlatformTypePtr(platformType),
				}

				db.Save(&cluster)
			}

			mockStatus := func() {
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
					eventstest.WithHostIdMatcher(host.ID.String()),
					eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
					eventstest.WithClusterIdMatcher(host.ClusterID.String()),
				))

				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostValidationFixedEventName),
					eventstest.WithHostIdMatcher(host.ID.String()),
					eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
					eventstest.WithClusterIdMatcher(host.ClusterID.String()),
				)).AnyTimes()

				mockDefaultClusterHostRequirements(mockHwValidator)
				mockHwValidator.EXPECT().IsValidStorageDeviceType(gomock.Any()).Return(true).AnyTimes()
				mockHwValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return([]*models.Disk{}).AnyTimes()
				mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("/dev/sda").AnyTimes()
			}

			It("Known to insufficient", func() {
				createHost(models.HostStatusKnown)
				createCluster()
				updateClusterPlatform(models.PlatformTypeVsphere)

				mockStatus()
				err := hapi.RefreshStatus(ctx, &host, db)
				Expect(err).ToNot(HaveOccurred())
				host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
				Expect(*host.Status).To(BeEquivalentTo(models.HostStatusInsufficient))

				// Flip it back
				mockStatus()
				updateClusterPlatform(models.PlatformTypeBaremetal)
				err = db.First(&cluster, "id = ?", clusterId).Error
				Expect(err).ShouldNot(HaveOccurred())
				err = hapi.RefreshStatus(ctx, &host, db)
				Expect(err).ToNot(HaveOccurred())
				host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
				Expect(*host.Status).To(BeEquivalentTo(models.HostStatusKnown))
			})

			It("Pending for input to insufficient", func() {
				createHost(models.HostStatusPendingForInput)
				createCluster()
				updateClusterPlatform(models.PlatformTypeVsphere)
				Expect(common.DeleteRecordsByClusterID(db, *cluster.ID, []interface{}{&models.MachineNetwork{}})).ShouldNot(HaveOccurred())
				err := db.First(&cluster, "id = ?", clusterId).Error
				Expect(err).ShouldNot(HaveOccurred())
				mockStatus()
				err = hapi.RefreshStatus(ctx, &host, db)
				Expect(err).ToNot(HaveOccurred())
				host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
				Expect(*host.Status).To(BeEquivalentTo(models.HostStatusInsufficient))

				// Flip it back
				mockStatus()
				updateClusterPlatform(models.PlatformTypeBaremetal)
				// The CIDR array rebuilt between the previous deletion to this stage
				Expect(common.DeleteRecordsByClusterID(db, *cluster.ID, []interface{}{&models.MachineNetwork{}})).ShouldNot(HaveOccurred())
				err = db.First(&cluster, "id = ?", clusterId).Error
				Expect(err).ShouldNot(HaveOccurred())
				err = hapi.RefreshStatus(ctx, &host, db)
				Expect(err).ToNot(HaveOccurred())
				host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
				Expect(*host.Status).To(BeEquivalentTo(models.HostStatusPendingForInput))
			})
		})
	})

	Context("host disconnected", func() {

		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
		})

		var srcState string

		//example of selected stages
		installationStages := []models.HostStage{
			models.HostStageStartingInstallation,
			models.HostStageWritingImageToDisk,
			models.HostStageWaitingForBootkube,
			models.HostStageWaitingForController,
		}

		for j := range installationStages {
			stage := installationStages[j]
			name := fmt.Sprintf("installationInProgress stage %s", stage)
			passedTime := 90 * time.Minute
			It(name, func() {
				srcState = models.HostStatusInstallingInProgress
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, srcState)
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
				cluster = hostutil.GenerateTestCluster(clusterId)
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
					eventstest.WithHostIdMatcher(hostId.String()),
					eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
					eventstest.WithClusterIdMatcher(host.ClusterID.String()),
					eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(models.HostStatusError))))
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
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, srcState)
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
				cluster = hostutil.GenerateTestCluster(clusterId)
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				err := hapi.RefreshStatus(ctx, &host, db)

				Expect(err).ToNot(HaveOccurred())
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())

				validationRes := make(ValidationsStatus)
				Expect(json.Unmarshal([]byte(resultHost.ValidationsInfo), &validationRes)).ToNot(HaveOccurred())
				for _, vRes := range validationRes {
					for _, v := range vRes {
						Expect(v.ID).ToNot(Equal(IsConnected))
						Expect(v.ID).ToNot(Equal(IsNTPSynced))
					}
				}
			})
		}
	})

	Context("host disconnected", func() {
		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
		})

		var srcState string

		passedTime := 90 * time.Minute
		It("host disconnected & preparing for installation", func() {
			srcState = models.HostStatusPreparingForInstallation
			host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, srcState)
			host.Inventory = hostutil.GenerateMasterInventory()
			host.Role = models.HostRoleMaster
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-MaxHostDisconnectionTime - time.Minute))

			progress := models.HostProgressInfo{
				StageStartedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
				StageUpdatedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
			}

			host.Progress = &progress

			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			cluster = hostutil.GenerateTestCluster(clusterId)
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(hostId.String()),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String()),
				eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(models.HostStatusDisconnected))))
			err := hapi.RefreshStatus(ctx, &host, db)

			Expect(err).ToNot(HaveOccurred())
			var resultHost models.Host
			Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
			Expect(swag.StringValue(resultHost.Status)).To(Equal(models.HostStatusDisconnected))
			info := statusInfoConnectionTimedOut
			Expect(swag.StringValue(resultHost.StatusInfo)).To(MatchRegexp(info))
		})
		It("host disconnected - bootstrap disconnection timeout is longer than regular host", func() {
			srcState = models.HostStatusInstalling
			host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, srcState)
			host.Inventory = hostutil.GenerateMasterInventory()
			host.Role = models.HostRoleMaster
			host.Bootstrap = true
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-MaxHostDisconnectionTime - 1*time.Minute))
			progress := models.HostProgressInfo{
				StageStartedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
				StageUpdatedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
			}

			host.Progress = &progress

			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			cluster = hostutil.GenerateTestCluster(clusterId)
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

			err := hapi.RefreshStatus(ctx, &host, db)

			Expect(err).ToNot(HaveOccurred())
			var resultHost models.Host
			Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
			Expect(swag.StringValue(resultHost.Status)).To(Equal(models.HostStatusInstalling))
		})
		It("host disconnected - bootstrap disconnection", func() {
			srcState = models.HostStatusPreparingForInstallation
			host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, srcState)
			host.Inventory = hostutil.GenerateMasterInventory()
			host.Role = models.HostRoleMaster
			host.Bootstrap = true
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-MaxHostDisconnectionTime - 2*time.Minute))
			progress := models.HostProgressInfo{
				StageStartedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
				StageUpdatedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
			}

			host.Progress = &progress

			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			cluster = hostutil.GenerateTestCluster(clusterId)
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(hostId.String()),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String()),
				eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(models.HostStatusDisconnected))))

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
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, srcState)
				host.Inventory = hostutil.GenerateMasterInventory()
				host.Role = models.HostRoleMaster
				host.CheckedInAt = hostCheckInAt
				host.StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-passedTime))

				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				cluster = hostutil.GenerateTestCluster(clusterId)
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
				if passedTimeKind == "over_timeout" {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(hostId.String()),
						eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
						eventstest.WithClusterIdMatcher(host.ClusterID.String()),
						eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(models.HostStatusError))))
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

		ClusterHighAvailabilityModes := []string{models.ClusterHighAvailabilityModeNone, models.ClusterHighAvailabilityModeFull}
		for i := range ClusterHighAvailabilityModes {
			highAvailabilityMode := ClusterHighAvailabilityModes[i]
			for j := range installationStages {
				stage := installationStages[j]
				for passedTimeKey, passedTimeValue := range timePassedTypes {
					name := fmt.Sprintf("installationInProgress stage %s %s", stage, passedTimeKey)
					passedTimeKind := passedTimeKey
					passedTime := passedTimeValue
					It(name, func() {
						hostCheckInAt := strfmt.DateTime(time.Now())
						srcState = models.HostStatusInstallingInProgress
						host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, srcState)
						host.Inventory = hostutil.GenerateMasterInventory()
						host.InstallationDiskPath = common.TestDiskId
						host.Role = models.HostRoleMaster
						host.CheckedInAt = hostCheckInAt
						progress := models.HostProgressInfo{
							CurrentStage:   stage,
							StageStartedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
							StageUpdatedAt: strfmt.DateTime(time.Now().Add(-passedTime)),
						}
						host.Progress = &progress
						Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
						cluster = hostutil.GenerateTestCluster(clusterId)
						cluster.HighAvailabilityMode = &highAvailabilityMode
						Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

						if passedTimeKind == "over_timeout" {
							if stage == models.HostStageRebooting {
								mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
									eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
									eventstest.WithHostIdMatcher(hostId.String()),
									eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
									eventstest.WithClusterIdMatcher(host.ClusterID.String()),
									eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(models.HostStatusInstallingPendingUserAction))))
							} else {
								mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
									eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
									eventstest.WithHostIdMatcher(hostId.String()),
									eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
									eventstest.WithClusterIdMatcher(host.ClusterID.String()),
									eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(models.HostStatusError))))
							}
						}
						err := hapi.RefreshStatus(ctx, &host, db)

						Expect(err).ToNot(HaveOccurred())
						var resultHost models.Host
						Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())

						if passedTimeKind == "under_timeout" {
							Expect(swag.StringValue(resultHost.Status)).To(Equal(models.HostStatusInstallingInProgress))
						} else {
							if stage == models.HostStageRebooting {
								Expect(swag.StringValue(resultHost.Status)).To(Equal(models.HostStatusInstallingPendingUserAction))
								statusInfo := strings.Replace(statusRebootTimeout, "$INSTALLATION_DISK", fmt.Sprintf("(test-disk, %s)", common.TestDiskId), 1)
								Expect(swag.StringValue(resultHost.StatusInfo)).To(Equal(statusInfo))
							} else {
								Expect(swag.StringValue(resultHost.Status)).To(Equal(models.HostStatusError))
								info := formatProgressTimedOutInfo(defaultConfig, stage)
								Expect(swag.StringValue(resultHost.StatusInfo)).To(Equal(info))
							}
						}
					})
				}
			}
		}
		It("state info progress when failed", func() {

			cluster = hostutil.GenerateTestCluster(clusterId)
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

			masterID := strfmt.UUID("1")
			master := hostutil.GenerateTestHost(masterID, infraEnvId, clusterId, models.HostStatusInstallingInProgress)
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
			host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusInstallingInProgress)
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

			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(models.HostStatusError)))).AnyTimes()

			err := hapi.RefreshStatus(ctx, &master, db)
			Expect(err).ToNot(HaveOccurred())

			err = hapi.RefreshStatus(ctx, &host, db)
			Expect(err).ToNot(HaveOccurred())

			var resultMaster models.Host
			Expect(db.Take(&resultMaster, "id = ? and cluster_id = ?", masterID.String(), clusterId.String()).Error).ToNot(HaveOccurred())

			var resultHost models.Host
			Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())

			info := formatProgressTimedOutInfo(defaultConfig, models.HostStageWaitingForControlPlane)
			Expect(swag.StringValue(resultMaster.StatusInfo)).To(Equal(info))

			info = formatProgressTimedOutInfo(defaultConfig, models.HostStageConfiguring)
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

			// cluster fields
			platformType models.PlatformType
		}{
			{
				name:          "insufficient worker memory",
				hostID:        strfmt.UUID("054e0100-f50e-4be7-874d-73861179e40d"),
				inventory:     hostutil.GenerateInventoryWithResourcesWithBytes(4, conversions.MibToBytes(150), conversions.MibToBytes(150), "worker"),
				role:          models.HostRoleWorker,
				srcState:      models.HostStatusDiscovering,
				dstState:      models.HostStatusInsufficient,
				statusInfoMsg: "Insufficient memory to deploy OpenShift Virtualization. Required memory is 360 MiB but found 150 MiB",
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					IsHostnameValid:                {status: ValidationSuccess, messagePattern: ""},
					BelongsToMajorityGroup:         {status: ValidationPending, messagePattern: "Machine Network CIDR or Connectivity Majority Groups missing"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster bec"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationFailure, messagePattern: "Require at least 8.35 GiB RAM for role worker, found only 150 MiB"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname worker is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					AreLsoRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: ""},
					AreOdfRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: "odf is disabled"},
					AreCnvRequirementsSatisfied:                    {status: ValidationFailure, messagePattern: "Insufficient memory to deploy OpenShift Virtualization. Required memory is 360 MiB but found 150 MiB"},
					CompatibleWithClusterPlatform:                  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
				}),
				hostRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, RAMMib: 8550},
			},
			{
				name:          "insufficient master memory",
				hostID:        strfmt.UUID("054e0100-f50e-4be7-874d-73861179e40d"),
				inventory:     hostutil.GenerateInventoryWithResourcesWithBytes(8, conversions.MibToBytes(100), conversions.MibToBytes(100), "master"),
				role:          models.HostRoleMaster,
				srcState:      models.HostStatusDiscovering,
				dstState:      models.HostStatusInsufficient,
				statusInfoMsg: "Insufficient memory to deploy OpenShift Virtualization. Required memory is 150 MiB but found 100 MiB",
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					IsHostnameValid:                {status: ValidationSuccess, messagePattern: ""},
					BelongsToMajorityGroup:         {status: ValidationPending, messagePattern: "Machine Network CIDR or Connectivity Majority Groups missing"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster bec"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:               {status: ValidationFailure, messagePattern: "Require at least 16.15 GiB RAM for role master, found only 100 MiB"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname master is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					AreLsoRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: ""},
					AreOdfRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: "odf is disabled"},
					AreCnvRequirementsSatisfied:                    {status: ValidationFailure, messagePattern: "Insufficient memory to deploy OpenShift Virtualization. Required memory is 150 MiB but found 100 MiB"},
					CompatibleWithClusterPlatform:                  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
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
				statusInfoMsg: "Insufficient CPU to deploy OpenShift Virtualization. Required CPU count is 2 but found 1",
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					IsHostnameValid:                {status: ValidationSuccess, messagePattern: ""},
					BelongsToMajorityGroup:         {status: ValidationPending, messagePattern: "Machine Network CIDR or Connectivity Majority Groups missing"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required CPU cores for any role is 2, found only 1"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationFailure, messagePattern: "Require at least 4 CPU cores for worker role, found only 1"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname worker is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					AreLsoRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: ""},
					AreOdfRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: "odf is disabled"},
					AreCnvRequirementsSatisfied:                    {status: ValidationFailure, messagePattern: "Insufficient CPU to deploy OpenShift Virtualization. Required CPU count is 2 but found 1"},
					CompatibleWithClusterPlatform:                  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
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
				statusInfoMsg: "Insufficient CPU to deploy OpenShift Virtualization. Required CPU count is 4 but found 1",
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					IsHostnameValid:                {status: ValidationSuccess, messagePattern: ""},
					BelongsToMajorityGroup:         {status: ValidationPending, messagePattern: "Machine Network CIDR or Connectivity Majority Groups missing"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required CPU cores for any role is 2, found only 1"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationFailure, messagePattern: "Require at least 8 CPU cores for master role, found only 1"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname master is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					AreLsoRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: ""},
					AreOdfRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: "odf is disabled"},
					AreCnvRequirementsSatisfied:                    {status: ValidationFailure, messagePattern: "Insufficient CPU to deploy OpenShift Virtualization. Required CPU count is 4 but found 1"},
					CompatibleWithClusterPlatform:                  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
				}),
				hostRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 8},
			},
			{
				name:          "insufficient virtualization cpu",
				hostID:        strfmt.UUID("054e0100-f50e-4be7-874d-73861179e40d"),
				inventory:     hostutil.GenerateMasterInventoryWithHostnameAndCpuFlags("master", []string{"fpu", "vme", "de", "pse", "tsc", "msr"}, "RHEL"),
				role:          models.HostRoleMaster,
				srcState:      models.HostStatusDiscovering,
				dstState:      models.HostStatusInsufficient,
				statusInfoMsg: "Hardware virtualization not supported",
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					IsHostnameValid:                {status: ValidationSuccess, messagePattern: ""},
					BelongsToMajorityGroup:         {status: ValidationPending, messagePattern: "Machine Network CIDR or Connectivity Majority Groups missing"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname master is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					AreLsoRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: ""},
					AreOdfRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: "odf is disabled"},
					AreCnvRequirementsSatisfied:                    {status: ValidationFailure, messagePattern: "CPU does not have virtualization support"},
					CompatibleWithClusterPlatform:                  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
				}),
			},
			{
				name:          "insufficient worker memory for host with GPU",
				hostID:        strfmt.UUID("054e0100-f50e-4be7-874d-73861179e40d"),
				inventory:     hostutil.GenerateInventoryWithResources(4, 1, "worker", &supportedGPU),
				role:          models.HostRoleWorker,
				srcState:      models.HostStatusDiscovering,
				dstState:      models.HostStatusInsufficient,
				statusInfoMsg: "Insufficient memory to deploy OpenShift Virtualization. Required memory is 1384 MiB but found 1024 MiB",
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					IsHostnameValid:                {status: ValidationSuccess, messagePattern: ""},
					BelongsToMajorityGroup:         {status: ValidationPending, messagePattern: "Machine Network CIDR or Connectivity Majority Groups missing"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster bec"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationFailure, messagePattern: "Require at least 9.35 GiB RAM for role worker, found only 1.00 GiB"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname worker is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					AreLsoRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: ""},
					AreOdfRequirementsSatisfied:                    {status: ValidationSuccess, messagePattern: "odf is disabled"},
					AreCnvRequirementsSatisfied:                    {status: ValidationFailure, messagePattern: "Insufficient memory to deploy OpenShift Virtualization. Required memory is 1384 MiB but found 1024 MiB"},
					CompatibleWithClusterPlatform:                  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
				}),
				hostRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, RAMMib: 9576},
			},
		}
		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				if t.platformType == "" {
					cluster = hostutil.GenerateTestCluster(clusterId)
				} else {
					cluster = hostutil.GenerateTestClusterWithPlatform(clusterId, common.TestIPv4Networking.MachineNetworks, &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)})
				}

				cluster.MonitoredOperators = []*models.MonitoredOperator{
					&lso.Operator,
					&cnv.Operator,
				}
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				host = hostutil.GenerateTestHost(t.hostID, infraEnvId, clusterId, t.srcState)
				host.Inventory = t.inventory
				host.Role = t.role
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				if t.hostRequirements != nil {
					mockHwValidator.EXPECT().GetClusterHostRequirements(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(&models.ClusterHostRequirements{Total: t.hostRequirements}, nil)
					mockPreflightHardwareRequirements(mockHwValidator, &defaultMasterRequirements, &defaultWorkerRequirements)
				} else {
					mockDefaultClusterHostRequirements(mockHwValidator)
				}
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()))).AnyTimes()

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

	getHost := func(infraEnvID, hostID strfmt.UUID) *models.Host {
		var h models.Host
		Expect(db.First(&h, "id = ? and infra_env_id = ?", hostID, infraEnvID).Error).ToNot(HaveOccurred())
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
			srcState    string
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
				name:              "Disk speed check failed after image availability",
				validCheckInTime:  true,
				dstState:          models.HostStatusInsufficient,
				clusterState:      models.ClusterStatusPreparingForInstallation,
				statusInfoChecker: makeRegexChecker("Host cannot be installed due to following failing validation.*While preparing the previous installation the installation disk speed measurement failed or was found to be insufficient"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationFailure, messagePattern: "While preparing the previous installation the installation disk speed measurement failed or was found to be insufficient"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationFailure, messagePattern: "Failed to fetch container images needed for installation from"},
				}),
				disksInfo:   createDiskInfo("/dev/sda", 0, -1),
				imageStatus: createFailedImageStatuses(),
			},
			{
				name:              "Image pull failed",
				validCheckInTime:  true,
				dstState:          models.HostStatusPreparingFailed,
				clusterState:      models.ClusterStatusPreparingForInstallation,
				statusInfoChecker: makeRegexChecker("Host failed to prepare for installation due to following failing validation.*Failed to fetch container images needed for installation from abc"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationFailure, messagePattern: "Failed to fetch container images needed for installation from"},
				}),
				imageStatus: createFailedImageStatuses(),
			},
			{
				name:              "Image pull failed and cluster moved to Ready",
				validCheckInTime:  true,
				srcState:          models.HostStatusPreparingFailed,
				dstState:          models.HostStatusKnown,
				clusterState:      models.ClusterStatusReady,
				statusInfoChecker: makeRegexChecker("Host is ready to be installed"),
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
				name:              "Disk speed should not run on if save partition was set",
				validCheckInTime:  true,
				dstState:          models.HostStatusPreparingSuccessful,
				clusterState:      models.ClusterStatusPreparingForInstallation,
				statusInfoChecker: makeValueChecker(statusInfoHostPreparationSuccessful),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				disksInfo:   "save_partition",
				imageStatus: createSuccessfulImageStatuses(),
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
				srcState := models.HostStatusPreparingForInstallation
				if t.srcState != "" {
					srcState = t.srcState
				}
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, srcState)
				host.StatusInfo = swag.String(statusInfoPreparingForInstallation)
				host.Inventory = hostutil.GenerateMasterInventoryWithHostname("master-0")
				host.CheckedInAt = hostCheckInAt
				host.DisksInfo = t.disksInfo
				host.ImagesStatus = t.imageStatus
				if t.disksInfo == "save_partition" {
					host.InstallerArgs = `["--save-partindex","5"]`
					host.DisksInfo = ""
				}
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

				// Test setup - Cluster creation
				machineCidr := common.TestIPv4Networking.MachineNetworks
				cluster = hostutil.GenerateTestClusterWithMachineNetworks(clusterId, machineCidr)
				cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", common.TestIPv4Networking.MachineNetworks[0].Cidr, hostId.String())
				cluster.Status = swag.String(t.clusterState)
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				// Test definition
				if t.dstState != models.HostStatusPreparingForInstallation {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(host.ID.String()),
						eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
						eventstest.WithClusterIdMatcher(host.ClusterID.String()),
						eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(t.dstState))))
				}
				Expect(getHost(infraEnvId, hostId).ValidationsInfo).To(BeEmpty())
				err := hapi.RefreshStatus(ctx, &host, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
					Expect(getHost(infraEnvId, hostId).ValidationsInfo).ToNot(BeEmpty())
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
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusPreparingSuccessful)
				host.StatusInfo = swag.String(statusInfoHostPreparationSuccessful)
				host.Inventory = hostutil.GenerateMasterInventoryWithHostname("master-0")
				host.CheckedInAt = hostCheckInAt
				host.DisksInfo = t.disksInfo
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

				// Test setup - Cluster creation
				machineCidr := common.TestIPv4Networking.MachineNetworks[0].Cidr
				cluster = hostutil.GenerateTestCluster(clusterId)
				cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", machineCidr, hostId.String())
				cluster.Status = swag.String(t.clusterState)
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				// Test definition
				if t.dstState != models.HostStatusPreparingSuccessful {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(hostId.String()),
						eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
						eventstest.WithClusterIdMatcher(host.ClusterID.String()),
						eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(t.dstState))))
				}
				Expect(getHost(infraEnvId, hostId).ValidationsInfo).To(BeEmpty())
				err := hapi.RefreshStatus(ctx, &host, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
					Expect(getHost(infraEnvId, hostId).ValidationsInfo).ToNot(BeEmpty())
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

		type TransitionTestStruct struct {
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
			machineNetworks       []*models.MachineNetwork
			serviceNetworks       []*models.ServiceNetwork
			connectivity          string
			addL3Connectivity     bool
			userManagedNetworking bool
			isDay2                bool

			numAdditionalHosts int
			operators          []*models.MonitoredOperator
			hostRequirements   *models.ClusterHostRequirementsDetails
			disksInfo          string

			machineNetworksForGroups []*models.MachineNetwork
			domainResolutions        *models.DomainResolutionResponse
			timestamp                int64
		}

		tests := []TransitionTestStruct{
			{
				name:              "discovering to disconnected",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  false,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationFailure, messagePattern: "Host is disconnected"},
					HasInventory:                   {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:                 {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:                   {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:               {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined:           {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:             {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:               {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:               {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr:           {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformNetworkSettingsValid: {status: ValidationPending, messagePattern: "Missing inventory"},
					CompatibleWithClusterPlatform:  {status: ValidationPending, messagePattern: "Missing inventory or platform isn't set"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "insufficient to disconnected",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  false,
				srcState:          models.HostStatusInsufficient,
				dstState:          models.HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationFailure, messagePattern: "Host is disconnected"},
					HasInventory:                   {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:                 {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:                   {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:               {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined:           {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:             {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:               {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:               {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr:           {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformNetworkSettingsValid: {status: ValidationPending, messagePattern: "Missing inventory"},
					CompatibleWithClusterPlatform:  {status: ValidationPending, messagePattern: "Missing inventory or platform isn't set"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "known to disconnected",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  false,
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
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
					IsConnected:                    {status: ValidationFailure, messagePattern: "Host is disconnected"},
					HasInventory:                   {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:                 {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:                   {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:               {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined:           {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:             {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:               {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:               {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr:           {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformNetworkSettingsValid: {status: ValidationPending, messagePattern: "Missing inventory"},
					CompatibleWithClusterPlatform:  {status: ValidationPending, messagePattern: "Missing inventory or platform isn't set"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "disconnected to disconnected",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  false,
				srcState:          models.HostStatusDisconnected,
				dstState:          models.HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationFailure, messagePattern: "Host is disconnected"},
					HasInventory:                   {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:                 {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:                   {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:               {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined:           {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:             {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:               {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:               {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr:           {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformNetworkSettingsValid: {status: ValidationPending, messagePattern: "Missing inventory"},
					CompatibleWithClusterPlatform:  {status: ValidationPending, messagePattern: "Missing inventory or platform isn't set"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
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
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "discovering to discovering",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  true,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusDiscovering,
				statusInfoChecker: makeValueChecker(statusInfoDiscovering),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:                 {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:                   {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:               {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined:           {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:             {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:               {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:               {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr:           {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformNetworkSettingsValid: {status: ValidationPending, messagePattern: "Missing inventory"},
					CompatibleWithClusterPlatform:  {status: ValidationPending, messagePattern: "Missing inventory or platform isn't set"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:             "disconnected to insufficient - auto-assign casted as worker (1)",
				role:             models.HostRoleAutoAssign,
				validCheckInTime: true,
				srcState:         models.HostStatusDisconnected,
				dstState:         models.HostStatusInsufficient,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs",
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes",
					"No eligible disks were found, please check specific disks to see why they are not eligible",
					"Require at least 8.00 GiB RAM for role worker, found only 130 bytes",
					"Host couldn't synchronize with any NTP server")),
				inventory: insufficientHWInventory(),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes"},
					HasMinValidDisks:               {status: ValidationFailure, messagePattern: "No eligible disks were found, please check specific disks to see why they are not eligible"},
					IsMachineCidrDefined:           {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationFailure, messagePattern: "Require at least 8.00 GiB RAM for role worker, found only 130 bytes"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname test is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
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
					"No eligible disks were found, please check specific disks to see why they are not eligible",
					"Require at least 8.00 GiB RAM for role worker, found only 130 bytes",
					"Host couldn't synchronize with any NTP server")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes"},
					HasMinValidDisks:               {status: ValidationFailure, messagePattern: "No eligible disks were found, please check specific disks to see why they are not eligible"},
					IsMachineCidrDefined:           {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationFailure, messagePattern: "Require at least 8.00 GiB RAM for role worker, found only 130 bytes"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname test is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:         insufficientHWInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:             "discovering to insufficient - auto-assign casted as worker (1)",
				role:             models.HostRoleAutoAssign,
				validCheckInTime: true,
				srcState:         models.HostStatusDiscovering,
				dstState:         models.HostStatusInsufficient,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs",
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes",
					"No eligible disks were found, please check specific disks to see why they are not eligible",
					"Require at least 8.00 GiB RAM for role worker, found only 130 bytes",
					"Host couldn't synchronize with any NTP server")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationFailure, messagePattern: "The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes"},
					HasMinValidDisks:               {status: ValidationFailure, messagePattern: "No eligible disks were found, please check specific disks to see why they are not eligible"},
					IsMachineCidrDefined:           {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role"},
					HasMemoryForRole:               {status: ValidationFailure, messagePattern: "Require at least 8.00 GiB RAM for role worker, found only 130 bytes"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname test is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:         insufficientHWInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "pending to insufficient (1)",
				role:              models.HostRoleAutoAssign,
				validCheckInTime:  true,
				srcState:          models.HostStatusPendingForInput,
				dstState:          models.HostStatusPendingForInput,
				statusInfoChecker: makeValueChecker(""),
				inventory:         insufficientHWInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     true,
			},
			{
				name:             "known to insufficient - auto-assign casted as worker (1)",
				role:             models.HostRoleAutoAssign,
				validCheckInTime: true,
				srcState:         models.HostStatusKnown,
				dstState:         models.HostStatusInsufficient,
				statusInfoChecker: makeValueChecker("Host does not meet the minimum hardware requirements: " +
					"Host couldn't synchronize with any NTP server ; Machine Network CIDR is undefined; the Machine " +
					"Network CIDR can be defined by setting either the API or Ingress virtual IPs ; " +
					"No eligible disks were found, please check specific disks to see why they are not eligible ; " +
					"Require at least 8.00 GiB RAM for role worker, found only 130 bytes ; " +
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for " +
					"any role is 8.00 GiB, found only 130 bytes"),
				inventory:         insufficientHWInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
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
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname worker-1 is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:         workerInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
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
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationFailure, messagePattern: "Machine Network CIDR is undefined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname worker-1 is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         workerInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:             "disconnected to insufficient (2)",
				validCheckInTime: true,
				srcState:         models.HostStatusDisconnected,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  testAdditionalMachineCidr,
				ntpSources:       []*models.NtpSource{common.TestNTPSourceUnsynced},
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesFailure},
				role:             models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDRs. Verify that the host belongs to every CIDR listed under machine networks",
					"Host couldn't synchronize with any NTP server",
					"Failed to fetch container images needed for installation from image. This may be due to a network hiccup. Retry to install again. If this problem persists, "+
						"check your network settings to make sure you’re not blocked.")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname worker-1 is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationFailure, messagePattern: "Host does not belong to machine network CIDRs. Verify that the host belongs to every CIDR listed under machine networks"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationFailure, messagePattern: "Failed to fetch container images needed for installation from image."},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:         workerInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:             "discovering to insufficient (2)",
				validCheckInTime: true,
				srcState:         models.HostStatusDiscovering,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  testAdditionalMachineCidr,
				ntpSources:       []*models.NtpSource{common.TestNTPSourceUnsynced},
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesFailure},
				role:             models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDRs. Verify that the host belongs to every CIDR listed under machine networks",
					"Require at least 4 CPU cores for master role, found only 2",
					"Require at least 16.00 GiB RAM for role master, found only 8.00 GiB",
					"Host couldn't synchronize with any NTP server",
					"Failed to fetch container images needed for installation from image. This may be due to a network hiccup. Retry to install again. If this problem persists, "+
						"check your network settings to make sure you’re not blocked.")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationFailure, messagePattern: "Require at least 4 CPU cores for master role, found only 2"},
					HasMemoryForRole:               {status: ValidationFailure, messagePattern: "Require at least 16.00 GiB RAM for role master, found only 8.00 GiB"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: "Hostname worker-1 is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationFailure, messagePattern: "Host does not belong to machine network CIDRs. Verify that the host belongs to every CIDR listed under machine networks"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationFailure, messagePattern: "Failed to fetch container images needed for installation from image."},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:         workerInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:             "discovering to known (openstack system vendor)",
				validCheckInTime: true,
				srcState:         models.HostStatusDiscovering,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  common.TestIPv4Networking.MachineNetworks,
				ntpSources:       defaultNTPSources,
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:             models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					fmt.Sprintf("Platform %s is allowed only for Single Node OpenShift or user-managed networking", OpenStackPlatform))),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:                {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsPlatformNetworkSettingsValid: {status: ValidationFailure, messagePattern: fmt.Sprintf("Platform %s is allowed only for Single Node OpenShift or user-managed networking", OpenStackPlatform)},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:         inventoryWithUnauthorizedVendor(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:             "insufficient to insufficient (2)",
				validCheckInTime: true,
				srcState:         models.HostStatusInsufficient,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  common.TestIPv4Networking.MachineNetworks,
				ntpSources:       defaultNTPSources,
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:             models.HostRoleMaster,
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
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname worker-1 is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:         workerInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:             "pending to insufficient (2)",
				validCheckInTime: true,
				srcState:         models.HostStatusPendingForInput,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  common.TestIPv4Networking.MachineNetworks,
				ntpSources:       defaultNTPSources,
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:             models.HostRoleMaster,
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
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname worker-1 is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:             "known to insufficient (2)",
				validCheckInTime: true,
				srcState:         models.HostStatusKnown,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  testAdditionalMachineCidr,
				ntpSources:       []*models.NtpSource{common.TestNTPSourceUnsynced},
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesFailure},
				role:             models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDRs. Verify that the host belongs to every CIDR listed under machine networks",
					"Host couldn't synchronize with any NTP server",
					"Failed to fetch container images needed for installation from image. This may be due to a network hiccup. Retry to install again. If this problem persists, "+
						"check your network settings to make sure you’re not blocked.")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                       {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                      {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                    {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                      {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:                  {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:              {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:                {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:                  {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:                  {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:              {status: ValidationFailure, messagePattern: "Host does not belong to machine network CIDRs"},
					IsNTPSynced:                       {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					IsTimeSyncedBetweenHostAndService: {status: ValidationSuccess, messagePattern: "Host clock is synchronized with service"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationFailure, messagePattern: "Failed to fetch container images needed for installation from image."},
				}),
				inventory:         hostutil.GenerateMasterInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:             "insufficient to insufficient (2)",
				validCheckInTime: true,
				srcState:         models.HostStatusInsufficient,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  testAdditionalMachineCidr,
				ntpSources:       []*models.NtpSource{common.TestNTPSourceUnsynced},
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesFailure},
				role:             models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDRs. Verify that the host belongs to every CIDR listed under machine networks",
					"Host couldn't synchronize with any NTP server",
					"Failed to fetch container images needed for installation from image. This may be due to a network hiccup. Retry to install again. If this problem persists, "+
						"check your network settings to make sure you’re not blocked.")),
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
					BelongsToMachineCidr: {status: ValidationFailure, messagePattern: "Host does not belong to machine network CIDRs"},
					IsNTPSynced:          {status: ValidationFailure, messagePattern: "Host couldn't synchronize with any NTP server"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationFailure, messagePattern: "Failed to fetch container images needed for installation from image."},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:         hostutil.GenerateMasterInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "discovering to known",
				validCheckInTime:  true,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                       {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                      {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                    {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                      {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:                  {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:              {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:                {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:                  {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:                  {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:              {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:                   {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:                       {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					IsTimeSyncedBetweenHostAndService: {status: ValidationSuccess, messagePattern: "Host clock is synchronized with service"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "discovering to insufficient - duplicate networks",
				validCheckInTime:  true,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusInsufficient,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				serviceNetworks:   common.TestIPv4Networking.ServiceNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleMaster,
				statusInfoChecker: makeRegexChecker("Address networks are overlapping"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:           {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:          {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:        {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:          {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:      {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:  {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:    {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:      {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:      {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:  {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:       {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:           {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					NonOverlappingSubnets: {status: ValidationFailure, messagePattern: "Address network .* appears on multiple interfaces"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventoryWithNetworks("1.2.3.4/24", "1.2.3.5/24"),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "discovering to insufficient - duplicate networks on same interface",
				validCheckInTime:  true,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				serviceNetworks:   common.TestIPv4Networking.ServiceNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleMaster,
				statusInfoChecker: makeRegexChecker("Host is ready to be installed"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:           {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:          {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:        {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:          {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:      {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:  {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:    {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:      {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:      {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:  {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:       {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:           {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					NonOverlappingSubnets: {status: ValidationSuccess, messagePattern: "Host subnets are not overlapping"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventoryWithNetworksOnSameInterface([]string{"1.2.3.4/24", "1.2.3.5/24"}, []string{}),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "discovering to insufficient - overlapping networks",
				validCheckInTime:  true,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusInsufficient,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				serviceNetworks:   common.TestIPv4Networking.ServiceNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleMaster,
				statusInfoChecker: makeRegexChecker("Address networks are overlapping"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:           {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:          {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:        {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:          {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:      {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:  {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:    {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:      {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:      {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:  {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:       {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:           {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					NonOverlappingSubnets: {status: ValidationFailure, messagePattern: "Address networks .* in interface .* and .* in interface .* overlap"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventoryWithNetworks("1.2.3.4/24", "1.2.3.16/23"),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "discovering to known - no overlapping networks",
				validCheckInTime:  true,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				serviceNetworks:   common.TestIPv4Networking.ServiceNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleMaster,
				statusInfoChecker: makeRegexChecker("Host is ready to be installed"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:           {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:          {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:        {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:          {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:      {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:  {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:    {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:      {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:      {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:  {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:       {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:           {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					NonOverlappingSubnets: {status: ValidationSuccess, messagePattern: "Host subnets are not overlapping"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventoryWithNetworks("1.2.3.4/24", "1.2.5.16/23"),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "discovering to known user managed networking",
				validCheckInTime:  true,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusKnown,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				role:              models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
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
					BelongsToMajorityGroup: {status: ValidationSuccess, messagePattern: "Host has connectivity to the majority of hosts in the cluster"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:             hostutil.GenerateMasterInventory(),
				domainResolutions:     common.TestDomainNameResolutionsSuccess,
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:              "discovering to known day2 cluster",
				validCheckInTime:  true,
				kind:              models.HostKindAddToExistingClusterHost,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusKnown,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				role:              models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:            {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:           {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:         {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:           {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:       {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:   {status: ValidationSuccess, messagePattern: "No Machine Network CIDR needed: Day2 cluster"},
					HasCPUCoresForRole:     {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:       {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:       {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "No machine network CIDR validation needed: Day2 cluster"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					BelongsToMajorityGroup: {status: ValidationSuccess, messagePattern: "Day2 host is not required to be connected to other hosts in the cluster"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
					IsDNSWildcardNotConfigured:                     {status: ValidationSuccess, messagePattern: "DNS wildcard check is not required for day2"},
				}),
				inventory:         hostutil.GenerateMasterInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
				isDay2:            true,
			},
			{
				name:              "discovering to insufficient user managed networking",
				validCheckInTime:  true,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusInsufficient,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				role:              models.HostRoleMaster,
				statusInfoChecker: makeRegexChecker("Host cannot be installed due to following failing validation"),
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
					BelongsToMajorityGroup: {status: ValidationPending, messagePattern: "Not enough hosts in cluster to calculate connectivity groups"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:             hostutil.GenerateMasterInventory(),
				domainResolutions:     common.TestDomainNameResolutionsSuccess,
				errorExpected:         false,
				userManagedNetworking: true,
				connectivity:          fmt.Sprintf("{\"%s\":[]}", network.IPv4.String()),
			},
			{
				name:              "discovering to insufficient user managed networking - 3 hosts",
				validCheckInTime:  true,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusInsufficient,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				role:              models.HostRoleMaster,
				statusInfoChecker: makeRegexChecker("Host cannot be installed due to following failing validation"),
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
					BelongsToMajorityGroup: {status: ValidationFailure, messagePattern: "No connectivity to the majority of hosts in the cluster"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:             hostutil.GenerateMasterInventory(),
				domainResolutions:     common.TestDomainNameResolutionsSuccess,
				errorExpected:         false,
				userManagedNetworking: true,
				connectivity:          fmt.Sprintf("{\"%s\":[]}", network.IPv4.String()),
				numAdditionalHosts:    2,
			},
			{
				name:              "discovering to insufficient - host time not synced",
				validCheckInTime:  true,
				srcState:          models.HostStatusDiscovering,
				dstState:          models.HostStatusInsufficient,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleWorker,
				statusInfoChecker: makeRegexChecker("Host cannot be installed due to following failing validation"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                       {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                      {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                    {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                      {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:                  {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:              {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:                {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:                  {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:                  {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:              {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:                   {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:                       {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					IsTimeSyncedBetweenHostAndService: {status: ValidationFailure, messagePattern: "Host clock is not synchronized, service time is ahead of host's"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:         hostutil.GenerateMasterInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
				timestamp:         time.Now().Add(-25 * time.Minute).Unix(),
			},
			{
				name:              "known to insufficient - host time not synced",
				validCheckInTime:  true,
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusInsufficient,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleWorker,
				statusInfoChecker: makeRegexChecker("Host cannot be installed due to following failing validation"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                       {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                      {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                    {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                      {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:                  {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:              {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:                {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:                  {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:                  {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:              {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:                   {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:                       {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					IsTimeSyncedBetweenHostAndService: {status: ValidationFailure, messagePattern: "Host clock is not synchronized, host time is ahead of service"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:         hostutil.GenerateMasterInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
				timestamp:         time.Now().Add(2 * time.Hour).Unix(),
			},
			{
				name:              "insufficient to known",
				validCheckInTime:  true,
				srcState:          models.HostStatusInsufficient,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                       {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                      {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                    {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                      {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:                  {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:              {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:                {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:                  {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:                  {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:              {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:                   {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:                       {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					IsTimeSyncedBetweenHostAndService: {status: ValidationSuccess, messagePattern: "Host clock is synchronized with service"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:         hostutil.GenerateMasterInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "insufficient to insufficient (failed disk info)",
				validCheckInTime:  true,
				srcState:          models.HostStatusInsufficient,
				dstState:          models.HostStatusInsufficient,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleWorker,
				statusInfoChecker: makeRegexChecker("Host cannot be installed due to following failing validation"),
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
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:      {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationFailure, messagePattern: "While preparing the previous installation the installation disk speed measurement failed or was found to be insufficient"},
				}),
				inventory:         hostutil.GenerateMasterInventory(),
				disksInfo:         createDiskInfo("/dev/sda", 0, -1),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "insufficient to known (successful disk info)",
				validCheckInTime:  true,
				srcState:          models.HostStatusInsufficient,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
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
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:      {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk is sufficient"},
				}),
				inventory:         hostutil.GenerateMasterInventory(),
				disksInfo:         createDiskInfo("/dev/sda", 10, 0),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "pending to known",
				validCheckInTime:  true,
				srcState:          models.HostStatusPendingForInput,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
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
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:      {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "pending to known IPv6",
				validCheckInTime:  true,
				srcState:          models.HostStatusPendingForInput,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv6Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
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
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:      {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventoryV6(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:                     "pending to known IPv6 - equivalent machine networks",
				validCheckInTime:         true,
				srcState:                 models.HostStatusPendingForInput,
				dstState:                 models.HostStatusKnown,
				machineNetworks:          common.TestIPv6Networking.MachineNetworks,
				machineNetworksForGroups: common.TestEquivalentIPv6Networking.MachineNetworks,
				ntpSources:               defaultNTPSources,
				imageStatuses:            map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:                     models.HostRoleWorker,
				statusInfoChecker:        makeValueChecker(statusInfoKnown),
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
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:      {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventoryV6(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:                     "pending to known IPv6 - equivalent machine networks with L3 connectivity",
				validCheckInTime:         true,
				srcState:                 models.HostStatusPendingForInput,
				dstState:                 models.HostStatusKnown,
				machineNetworks:          common.TestIPv6Networking.MachineNetworks,
				machineNetworksForGroups: common.TestEquivalentIPv6Networking.MachineNetworks,
				ntpSources:               defaultNTPSources,
				imageStatuses:            map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:                     models.HostRoleWorker,
				statusInfoChecker:        makeValueChecker(statusInfoKnown),
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
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:      {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventoryV6(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
				addL3Connectivity: true,
			},
			{
				name:                     "pending to insufficient IPv6 - IPv4 connectivity groups",
				validCheckInTime:         true,
				srcState:                 models.HostStatusPendingForInput,
				dstState:                 models.HostStatusInsufficient,
				machineNetworks:          common.TestIPv6Networking.MachineNetworks,
				machineNetworksForGroups: common.TestIPv4Networking.MachineNetworks,
				ntpSources:               defaultNTPSources,
				imageStatuses:            map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:                     models.HostRoleWorker,
				statusInfoChecker:        makeRegexChecker("Host cannot be installed due to following failing validation"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:            {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:           {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:         {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:           {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:       {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:   {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:     {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:       {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:       {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					BelongsToMajorityGroup: {status: ValidationPending, messagePattern: "Not enough hosts in cluster to calculate connectivity groups"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:            {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventoryV6(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "pending to known IPv6 - with L3 connectivity key",
				validCheckInTime:  true,
				srcState:          models.HostStatusPendingForInput,
				dstState:          models.HostStatusInsufficient,
				machineNetworks:   common.TestIPv6Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleWorker,
				statusInfoChecker: makeRegexChecker("Host cannot be installed due to following failing validation"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:            {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:           {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:         {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:           {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:       {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:   {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:     {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:       {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:       {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					BelongsToMajorityGroup: {status: ValidationPending, messagePattern: "Not enough hosts in cluster to calculate connectivity groups"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					IsNTPSynced:            {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventoryV6(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
				connectivity:      fmt.Sprintf("{\"%s\":[]}", network.IPv4.String()),
			},
			{
				name:              "known to known",
				validCheckInTime:  true,
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
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
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					BelongsToMajorityGroup: {status: ValidationSuccess, messagePattern: "Host has connectivity to the majority of hosts in the cluster"},
					IsNTPSynced:            {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:              "known to insufficient",
				validCheckInTime:  true,
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusInsufficient,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall)),
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
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					BelongsToMajorityGroup: {status: ValidationPending, messagePattern: "Not enough hosts in cluster to calculate connectivity groups"},
					IsNTPSynced:            {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventory(),
				connectivity:      fmt.Sprintf("{\"%s\":[]}", common.TestIPv4Networking.MachineNetworks[0].Cidr),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:             "known to insufficient + additional hosts",
				validCheckInTime: true,
				srcState:         models.HostStatusKnown,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  common.TestIPv4Networking.MachineNetworks,
				ntpSources:       defaultNTPSources,
				role:             models.HostRoleMaster,
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
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					BelongsToMajorityGroup: {status: ValidationFailure, messagePattern: "No connectivity to the majority of hosts in the cluster"},
					IsNTPSynced:            {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:          hostutil.GenerateMasterInventory(),
				connectivity:       fmt.Sprintf("{\"%s\":[]}", common.TestIPv4Networking.MachineNetworks[0].Cidr),
				domainResolutions:  common.TestDomainNameResolutionsSuccess,
				errorExpected:      false,
				numAdditionalHosts: 2,
			},
			{
				name:              "known to known + additional hosts - dual-stack cluster, dual-stack hosts",
				validCheckInTime:  true,
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestDualStackNetworking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				role:              models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
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
					BelongsToMachineCidr:   {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					BelongsToMajorityGroup: {status: ValidationSuccess, messagePattern: "Host has connectivity to the majority of hosts in the cluster"},
					IsNTPSynced:            {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:          hostutil.GenerateMasterInventoryDualStack(),
				domainResolutions:  common.TestDomainNameResolutionsSuccess,
				errorExpected:      false,
				numAdditionalHosts: 2,
			},
			{
				name:             "known to insufficient + additional hosts - dual-stack cluster, v4 hosts",
				validCheckInTime: true,
				srcState:         models.HostStatusKnown,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  common.TestDualStackNetworking.MachineNetworks,
				ntpSources:       defaultNTPSources,
				role:             models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Host does not belong to machine network CIDRs. Verify that the host belongs to every CIDR listed under machine networks")),
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
					BelongsToMachineCidr:   {status: ValidationFailure, messagePattern: "Host does not belong to machine network CIDRs. Verify that the host belongs to every CIDR listed under machine networks"},
					IsHostnameValid:        {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
					BelongsToMajorityGroup: {status: ValidationSuccess, messagePattern: "Host has connectivity to the majority of hosts in the cluster"},
					IsNTPSynced:            {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
				}),
				inventory:          hostutil.GenerateMasterInventory(),
				domainResolutions:  common.TestDomainNameResolutionsSuccess,
				errorExpected:      false,
				numAdditionalHosts: 2,
			},
			{
				name:              "known to known with unexpected role",
				validCheckInTime:  true,
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				role:              "kuku",
				statusInfoChecker: makeValueChecker(""),
				inventory:         hostutil.GenerateMasterInventory(),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     true,
			},
			{
				name:              "AddedtoExistingCluster to AddedtoExistingCluster for day2 cloud",
				srcState:          models.HostStatusAddedToExistingCluster,
				dstState:          models.HostStatusAddedToExistingCluster,
				kind:              models.HostKindAddToExistingClusterHost,
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(""),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			},
			{
				name:             "CNV + LSO enabled: known to insufficient with 1 worker",
				validCheckInTime: true,
				srcState:         models.HostStatusKnown,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  common.TestIPv4Networking.MachineNetworks,
				ntpSources:       defaultNTPSources,
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:             models.HostRoleWorker,
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
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				domainResolutions:  common.TestDomainNameResolutionsSuccess,
				errorExpected:      false,
				operators:          []*models.MonitoredOperator{&cnv.Operator, &lso.Operator},
				numAdditionalHosts: 0,
				hostRequirements:   &models.ClusterHostRequirementsDetails{CPUCores: 4, RAMMib: 8192},
			},
			{
				name:             "CNV + LSO enabled: insufficient to insufficient with 1 master",
				validCheckInTime: true,
				srcState:         models.HostStatusInsufficient,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  common.TestIPv4Networking.MachineNetworks,
				ntpSources:       defaultNTPSources,
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:             models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Insufficient CPU to deploy OpenShift Virtualization. Required CPU count is 4 but found 1 ",
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
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				domainResolutions:  common.TestDomainNameResolutionsSuccess,
				errorExpected:      false,
				operators:          []*models.MonitoredOperator{&cnv.Operator, &lso.Operator},
				numAdditionalHosts: 0,
				hostRequirements:   &models.ClusterHostRequirementsDetails{CPUCores: 8, RAMMib: 16537},
			},
			{
				name:              "CNV + LSO enabled: known to known with sufficient CPU and memory with 3 master",
				validCheckInTime:  true,
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoKnown)),
				inventory:         hostutil.GenerateInventoryWithResources(8, 17, "master-1"),
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
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				domainResolutions:  common.TestDomainNameResolutionsSuccess,
				errorExpected:      false,
				operators:          []*models.MonitoredOperator{&cnv.Operator, &lso.Operator},
				numAdditionalHosts: 2,
			},
			{
				name:              "CNV + OCS enabled: known to known with sufficient CPU and memory with 3 master",
				validCheckInTime:  true,
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				ntpSources:        defaultNTPSources,
				imageStatuses:     map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:              models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoKnown)),
				inventory:         hostutil.GenerateInventoryWithResources(18, 64, "master-1"),
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
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				domainResolutions:  common.TestDomainNameResolutionsSuccess,
				errorExpected:      false,
				operators:          []*models.MonitoredOperator{&cnv.Operator, &lso.Operator},
				numAdditionalHosts: 2,
			},
			{
				name:             "CNV + OCS enabled: known to insufficient with lack of memory with 3 workers",
				validCheckInTime: true,
				srcState:         models.HostStatusKnown,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  common.TestIPv4Networking.MachineNetworks,
				ntpSources:       defaultNTPSources,
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:             models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Require at least 8.35 GiB RAM for role worker, found only 8.00 GiB",
					"ODF unsupported Host Role for Compact Mode.")),
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
					BelongsToMachineCidr:        {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsNTPSynced:                 {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					AreOdfRequirementsSatisfied: {status: ValidationFailure, messagePattern: "ODF unsupported Host Role for Compact Mode."},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				domainResolutions:  common.TestDomainNameResolutionsSuccess,
				errorExpected:      false,
				operators:          []*models.MonitoredOperator{&cnv.Operator, &lso.Operator, &odf.Operator},
				numAdditionalHosts: 2,
				hostRequirements:   &models.ClusterHostRequirementsDetails{CPUCores: 4, RAMMib: 8550},
			},
			{
				name:             "discovering to insufficient user managed networking domain api address is same as a node address v4",
				validCheckInTime: true,
				srcState:         models.HostStatusDiscovering,
				dstState:         models.HostStatusInsufficient,
				ntpSources:       defaultNTPSources,
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				machineNetworks:  common.TestIPv4Networking.MachineNetworks,
				role:             models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					fmt.Sprintf("Domain %s must not point at %s as it is the API address of this host. This domain must instead point at the IP address of a load balancer when using user managed networking in a multi control-plane node cluster", common.DomainAPI, "1.2.3.40/24"),
					fmt.Sprintf("Domain %s must not point at %s as it is the API address of this host. This domain must instead point at the IP address of a load balancer when using user managed networking in a multi control-plane node cluster", common.DomainAPIInternal, "4.5.6.7/24"))),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsAPIDomainNameResolvedCorrectly:         {status: ValidationFailure, messagePattern: fmt.Sprintf("Domain %s must not point at %s as it is the API address of this host. This domain must instead point at the IP address of a load balancer when using user managed networking in a multi control-plane node cluster", common.DomainAPI, "1.2.3.40/24")},
					IsAPIInternalDomainNameResolvedCorrectly: {status: ValidationFailure, messagePattern: fmt.Sprintf("Domain %s must not point at %s as it is the API address of this host. This domain must instead point at the IP address of a load balancer when using user managed networking in a multi control-plane node cluster", common.DomainAPIInternal, "4.5.6.7/24")},
				}),
				inventory:             hostutil.GenerateMasterInventoryWithNetworksOnSameInterface([]string{"1.2.3.40/24", "4.5.6.7/24"}, []string{}),
				domainResolutions:     common.TestDomainNameResolutionsSuccess,
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:             "discovering to insufficient user managed networking domain api address is same as a node address v6",
				validCheckInTime: true,
				srcState:         models.HostStatusDiscovering,
				dstState:         models.HostStatusInsufficient,
				ntpSources:       defaultNTPSources,
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				machineNetworks:  common.TestIPv4Networking.MachineNetworks,
				role:             models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					fmt.Sprintf("Domain %s must not point at %s as it is the API address of this host. This domain must instead point at the IP address of a load balancer when using user managed networking in a multi control-plane node cluster", common.DomainAPI, "1001:db8::20/120"),
					fmt.Sprintf("Domain %s must not point at %s as it is the API address of this host. This domain must instead point at the IP address of a load balancer when using user managed networking in a multi control-plane node cluster", common.DomainAPIInternal, "1002:db8::30/120"))),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsAPIDomainNameResolvedCorrectly:         {status: ValidationFailure, messagePattern: fmt.Sprintf("Domain %s must not point at %s as it is the API address of this host. This domain must instead point at the IP address of a load balancer when using user managed networking in a multi control-plane node cluster", common.DomainAPI, "1001:db8::20/120")},
					IsAPIInternalDomainNameResolvedCorrectly: {status: ValidationFailure, messagePattern: fmt.Sprintf("Domain %s must not point at %s as it is the API address of this host. This domain must instead point at the IP address of a load balancer when using user managed networking in a multi control-plane node cluster", common.DomainAPIInternal, "1002:db8::30/120")},
				}),
				inventory:             hostutil.GenerateMasterInventoryWithNetworksOnSameInterface([]string{}, []string{"1001:db8::20/120", "1002:db8::30/120"}),
				domainResolutions:     common.TestDomainNameResolutionsSuccess,
				errorExpected:         false,
				userManagedNetworking: true,
			},
			{
				name:             "discovering to insufficient user managed networking domain api addresses not defined",
				validCheckInTime: true,
				srcState:         models.HostStatusDiscovering,
				dstState:         models.HostStatusInsufficient,
				ntpSources:       defaultNTPSources,
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				machineNetworks:  common.TestIPv4Networking.MachineNetworks,
				role:             models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					fmt.Sprintf("Couldn't resolve domain name %s on the host. To continue installation, create the necessary DNS entries to resolve this domain name to your cluster's %s IP address", common.DomainAPI, "API load balancer"),
					fmt.Sprintf("Couldn't resolve domain name %s on the host. To continue installation, create the necessary DNS entries to resolve this domain name to your cluster's %s IP address", common.DomainAPIInternal, "internal API load balancer"))),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsAPIDomainNameResolvedCorrectly:         {status: ValidationFailure, messagePattern: fmt.Sprintf("Couldn't resolve domain name %s on the host. To continue installation, create the necessary DNS entries to resolve this domain name to your cluster's %s IP address", common.DomainAPI, "API load balancer")},
					IsAPIInternalDomainNameResolvedCorrectly: {status: ValidationFailure, messagePattern: fmt.Sprintf("Couldn't resolve domain name %s on the host. To continue installation, create the necessary DNS entries to resolve this domain name to your cluster's %s IP address", common.DomainAPIInternal, "internal API load balancer")},
				}),
				inventory:             hostutil.GenerateMasterInventoryWithNetworksOnSameInterface([]string{"1.2.3.40/24", "4.5.6.7/24"}, []string{"1001:db8::10/120", "1002:db8::10/120"}),
				domainResolutions:     common.TestDomainResolutionsNoAPI,
				errorExpected:         false,
				userManagedNetworking: true,
			},
		}

		for _, hostTest := range []struct {
			hostName      string
			isBlackListed bool
		}{
			{"localhost", true},
			{"localhost.localdomain", true},
			{"localhost4", true},
			{"localhost4.localdomain4", true},
			{"localhost6", true},
			{"localhost6.localdomain6", true},
			{"AAAA.com", false},
			{"redhat.com.", false},
		} {
			statusInfoCheckerMessage := fmt.Sprintf("Hostname %s is forbidden, hostname should match pattern %s", hostTest.hostName, hostutil.HostnamePattern)
			isHostNameValidMessage := fmt.Sprintf("Hostname %s is forbidden, hostname should match pattern", hostTest.hostName)

			if hostTest.isBlackListed {
				statusInfoCheckerMessage = fmt.Sprintf("The host name %s is forbidden", hostTest.hostName)
				isHostNameValidMessage = fmt.Sprintf("The host name %s is forbidden", hostTest.hostName)
			}

			tests = append(tests, TransitionTestStruct{
				name:             fmt.Sprintf("insufficient to insufficient (%s)", hostTest.hostName),
				validCheckInTime: true,
				srcState:         models.HostStatusInsufficient,
				dstState:         models.HostStatusInsufficient,
				machineNetworks:  common.TestIPv4Networking.MachineNetworks,
				ntpSources:       defaultNTPSources,
				imageStatuses:    map[string]*models.ContainerImageAvailability{common.TestDefaultConfig.ImageName: common.TestImageStatusesSuccess},
				role:             models.HostRoleMaster,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					statusInfoCheckerMessage)),
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
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsHostnameValid:      {status: ValidationFailure, messagePattern: isHostNameValidMessage},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
					SufficientOrUnknownInstallationDiskSpeed:       {status: ValidationSuccess, messagePattern: "Speed of installation disk has not yet been measured"},
				}),
				inventory:         hostutil.GenerateMasterInventoryWithHostname(hostTest.hostName),
				domainResolutions: common.TestDomainNameResolutionsSuccess,
				errorExpected:     false,
			})
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
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, srcState)
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
				bytes, err = json.Marshal(t.domainResolutions)
				Expect(err).ShouldNot(HaveOccurred())
				host.DomainNameResolutions = string(bytes)
				if t.timestamp == 0 {
					t.timestamp = time.Now().Unix()
				}
				host.Timestamp = t.timestamp

				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

				for i := 0; i < t.numAdditionalHosts; i++ {
					id := strfmt.UUID(uuid.New().String())
					h := hostutil.GenerateTestHost(id, infraEnvId, clusterId, srcState)
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
				clusterKind := models.ClusterKindCluster
				if t.isDay2 {
					clusterKind = models.ClusterKindAddHostsCluster
				}

				cluster = hostutil.GenerateTestClusterWithMachineNetworks(clusterId, t.machineNetworks)
				cluster.Kind = &clusterKind
				cluster.ServiceNetworks = t.serviceNetworks
				cluster.UserManagedNetworking = &t.userManagedNetworking
				cluster.Name = common.TestDefaultConfig.ClusterName
				cluster.BaseDNSDomain = common.TestDefaultConfig.BaseDNSDomain
				if t.connectivity == "" {
					if t.userManagedNetworking {
						cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", network.IPv4.String(), hostId.String())
					} else {
						machineNetworks := t.machineNetworks
						if t.machineNetworksForGroups != nil {
							machineNetworks = t.machineNetworksForGroups
						}
						cluster.ConnectivityMajorityGroups = generateMajorityGroup(machineNetworks, hostId)
					}
				} else {
					cluster.ConnectivityMajorityGroups = t.connectivity
				}
				if t.addL3Connectivity {
					cluster.ConnectivityMajorityGroups = addL3Connectivity(cluster.ConnectivityMajorityGroups, hostId)
				}

				cluster.MonitoredOperators = t.operators
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				// Test definition
				if srcState != t.dstState {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(hostId.String()),
						eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
						eventstest.WithClusterIdMatcher(host.ClusterID.String()),
						eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(t.dstState))))
				}
				Expect(getHost(infraEnvId, hostId).ValidationsInfo).To(BeEmpty())
				err = hapi.RefreshStatus(ctx, &host, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
					Expect(getHost(infraEnvId, hostId).ValidationsInfo).ToNot(BeEmpty())
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
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusPreparingForInstallation)
				host.Inventory = hostutil.GenerateMasterInventory()
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				cluster = hostutil.GenerateTestCluster(clusterId)
				cluster.Status = &t.clusterStatus
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
				if *host.Status != t.dstState {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(hostId.String()),
						eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
						eventstest.WithClusterIdMatcher(host.ClusterID.String()),
						eventstest.WithSeverityMatcher(hostutil.GetEventSeverityFromHostStatus(t.dstState))))
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
			machineNetworks []*models.MachineNetwork

			// 2nd Host fields
			otherState             string
			otherRequestedHostname string
			otherInventory         string
		}{
			{
				name:              "insufficient to known",
				srcState:          models.HostStatusInsufficient,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
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
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsNTPSynced:          {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"}}),
				inventory:      hostutil.GenerateMasterInventoryWithHostname("first"),
				otherState:     models.HostStatusInsufficient,
				otherInventory: hostutil.GenerateMasterInventoryWithHostname("second"),
				errorExpected:  false,
			},
			{
				name:            "insufficient to insufficient (same hostname) 1",
				srcState:        models.HostStatusInsufficient,
				dstState:        models.HostStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				role:            models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname first is not unique in cluster")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:      hostutil.GenerateMasterInventoryWithHostname("first"),
				otherState:     models.HostStatusInsufficient,
				otherInventory: hostutil.GenerateMasterInventoryWithHostname("first"),
				errorExpected:  false,
			},
			{
				name:            "insufficient to insufficient (same hostname) 2",
				srcState:        models.HostStatusInsufficient,
				dstState:        models.HostStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				role:            models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname first is not unique in cluster")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:              hostutil.GenerateMasterInventoryWithHostname("first"),
				otherState:             models.HostStatusInsufficient,
				otherInventory:         hostutil.GenerateMasterInventoryWithHostname("second"),
				otherRequestedHostname: "first",
				errorExpected:          false,
			},
			{
				name:            "insufficient to insufficient (same hostname) 3",
				srcState:        models.HostStatusInsufficient,
				dstState:        models.HostStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				role:            models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname second is not unique in cluster")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventoryWithHostname("first"),
				requestedHostname: "second",
				otherState:        models.HostStatusInsufficient,
				otherInventory:    hostutil.GenerateMasterInventoryWithHostname("second"),
				errorExpected:     false,
			},
			{
				name:            "insufficient to insufficient (same hostname) 4 loveeee",
				srcState:        models.HostStatusInsufficient,
				dstState:        models.HostStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				role:            models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname third is not unique in cluster")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
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
				name:              "insufficient to known 2",
				srcState:          models.HostStatusInsufficient,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
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
				name:              "known to known",
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"}}),
				inventory:      hostutil.GenerateMasterInventoryWithHostname("first"),
				otherState:     models.HostStatusInsufficient,
				otherInventory: hostutil.GenerateMasterInventoryWithHostname("second"),
				errorExpected:  false,
			},
			{
				name:            "known to insufficient (same hostname) 1",
				srcState:        models.HostStatusKnown,
				dstState:        models.HostStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				role:            models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname first is not unique in cluster")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:      hostutil.GenerateMasterInventoryWithHostname("first"),
				otherState:     models.HostStatusInsufficient,
				otherInventory: hostutil.GenerateMasterInventoryWithHostname("first"),
				errorExpected:  false,
			},
			{
				name:            "known to insufficient (same hostname) 2",
				srcState:        models.HostStatusKnown,
				dstState:        models.HostStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				role:            models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname first is not unique in cluster")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:              hostutil.GenerateMasterInventoryWithHostname("first"),
				otherState:             models.HostStatusInsufficient,
				otherInventory:         hostutil.GenerateMasterInventoryWithHostname("second"),
				otherRequestedHostname: "first",
				errorExpected:          false,
			},
			{
				name:            "known to insufficient (same hostname) 3",
				srcState:        models.HostStatusKnown,
				dstState:        models.HostStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				role:            models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname second is not unique in cluster")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"},
				}),
				inventory:         hostutil.GenerateMasterInventoryWithHostname("first"),
				requestedHostname: "second",
				otherState:        models.HostStatusInsufficient,
				otherInventory:    hostutil.GenerateMasterInventoryWithHostname("second"),
				errorExpected:     false,
			},
			{
				name:            "known to insufficient (same hostname) 4",
				srcState:        models.HostStatusKnown,
				dstState:        models.HostStatusInsufficient,
				machineNetworks: common.TestIPv4Networking.MachineNetworks,
				role:            models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoNotReadyForInstall,
					"Hostname third is not unique in cluster")),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					HasCPUCoresForRole:             {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:               {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:               {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
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
				name:              "known to known 2",
				srcState:          models.HostStatusKnown,
				dstState:          models.HostStatusKnown,
				machineNetworks:   common.TestIPv4Networking.MachineNetworks,
				role:              models.HostRoleWorker,
				statusInfoChecker: makeValueChecker(statusInfoKnown),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:                    {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:                   {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:                 {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:                   {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:               {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined:           {status: ValidationSuccess, messagePattern: "Machine Network CIDR is defined"},
					IsHostnameUnique:               {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr:           {status: ValidationSuccess, messagePattern: "Host belongs to all machine network CIDRs"},
					IsPlatformNetworkSettingsValid: {status: ValidationSuccess, messagePattern: "Platform RHEL is allowed"},
					CompatibleWithClusterPlatform:  {status: ValidationSuccess, messagePattern: "Host is compatible with cluster platform baremetal"},
					IsNTPSynced:                    {status: ValidationSuccess, messagePattern: "Host NTP is synced"},
					SucessfullOrUnknownContainerImagesAvailability: {status: ValidationSuccess, messagePattern: "All required container images were either pulled successfully or no attempt was made to pull them"}}),
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
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, srcState)
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
				domainNameResolutions := common.TestDomainNameResolutionsSuccess
				bytes, err = json.Marshal(domainNameResolutions)
				Expect(err).ShouldNot(HaveOccurred())
				host.DomainNameResolutions = string(bytes)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

				// Test setup - 2nd Host creation
				otherHost := hostutil.GenerateTestHost(otherHostID, infraEnvId, clusterId, t.otherState)
				otherHost.RequestedHostname = t.otherRequestedHostname
				otherHost.Inventory = t.otherInventory
				Expect(db.Create(&otherHost).Error).ShouldNot(HaveOccurred())

				// Test setup - Cluster creation
				cluster = hostutil.GenerateTestClusterWithMachineNetworks(clusterId, t.machineNetworks)
				cluster.ConnectivityMajorityGroups = generateMajorityGroup(t.machineNetworks, hostId)
				cluster.Name = common.TestDefaultConfig.ClusterName
				cluster.BaseDNSDomain = common.TestDefaultConfig.BaseDNSDomain
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				// Test definition
				expectedSeverity := models.EventSeverityInfo
				if t.dstState == models.HostStatusInsufficient {
					expectedSeverity = models.EventSeverityWarning
				}
				if !t.errorExpected && srcState != t.dstState {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(host.ID.String()),
						eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
						eventstest.WithClusterIdMatcher(host.ClusterID.String()),
						eventstest.WithSeverityMatcher(expectedSeverity)))
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
					h := hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, srcState)
					h.Progress.CurrentStage = installationStage
					h.Inventory = hostutil.GenerateMasterInventory()
					Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
					c := hostutil.GenerateTestCluster(clusterId)
					c.Status = swag.String(models.ClusterStatusInstalling)
					Expect(db.Create(&c).Error).ToNot(HaveOccurred())

					err := hapi.RefreshStatus(ctx, &h, db)

					Expect(err).ShouldNot(HaveOccurred())
					Expect(swag.StringValue(h.Status)).Should(Equal(srcState))
				})
				It(fmt.Sprintf("host src: %s cluster error: true", srcState), func() {
					h := hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, srcState)
					h.Progress.CurrentStage = installationStage
					h.Inventory = hostutil.GenerateMasterInventory()
					Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
					c := hostutil.GenerateTestCluster(clusterId)
					c.Status = swag.String(models.ClusterStatusError)
					Expect(db.Create(&c).Error).ToNot(HaveOccurred())

					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(hostId.String()),
						eventstest.WithInfraEnvIdMatcher(infraEnvId.String()),
						eventstest.WithClusterIdMatcher(clusterId.String()),
						eventstest.WithSeverityMatcher(models.EventSeverityError)))

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
			cluster = hostutil.GenerateTestCluster(clusterId)
			cluster.Name = common.TestDefaultConfig.ClusterName
			cluster.BaseDNSDomain = common.TestDefaultConfig.BaseDNSDomain
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusDiscovering)
			host.Inventory = hostutil.GenerateInventoryWithResourcesWithBytes(4, conversions.GibToBytes(16), conversions.GibToBytes(16), "master")
			host.Role = models.HostRoleMaster
			host.NtpSources = string(defaultNTPSourcesInBytes)
			domainNameResolutions := common.TestDomainNameResolutionsSuccess
			bytes, err := json.Marshal(domainNameResolutions)
			Expect(err).ShouldNot(HaveOccurred())
			host.DomainNameResolutions = string(bytes)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(hostId.String()),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String()))).AnyTimes()
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
				hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, nil, defaultConfig, nil, operatorsManager, pr, false, nil)

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
	Context("Platform validations", func() {

		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(hostId.String()),
				eventstest.WithClusterIdMatcher(clusterId.String()))).AnyTimes()
		})

		tests := []struct {
			name                  string
			hostPlatform          string
			userManagedNetworking bool
			dstState              string
		}{
			{
				name:                  fmt.Sprintf("validate %s and userManagedNetwork false", OpenStackPlatform),
				hostPlatform:          OpenStackPlatform,
				userManagedNetworking: false,
				dstState:              models.HostStatusInsufficient,
			},
			{
				name:                  fmt.Sprintf("validate %s and userManagedNetwork true", OpenStackPlatform),
				hostPlatform:          OpenStackPlatform,
				userManagedNetworking: true,
				dstState:              models.HostStatusKnown,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusDiscovering)
				host.Inventory = hostutil.GenerateMasterInventoryWithSystemPlatform(t.hostPlatform)
				host.Role = models.HostRoleMaster
				defaultNTPSourcesInBytes, err := json.Marshal(defaultNTPSources)
				Expect(err).ShouldNot(HaveOccurred())
				host.NtpSources = string(defaultNTPSourcesInBytes)
				defaultDomainNameResolutions := common.TestDomainNameResolutionsSuccess
				domainNameResolutions, err := json.Marshal(defaultDomainNameResolutions)
				Expect(err).ShouldNot(HaveOccurred())
				host.DomainNameResolutions = string(domainNameResolutions)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				cluster = hostutil.GenerateTestCluster(clusterId)
				cluster.UserManagedNetworking = swag.Bool(t.userManagedNetworking)
				cluster.Name = common.TestDefaultConfig.ClusterName
				cluster.BaseDNSDomain = common.TestDefaultConfig.BaseDNSDomain
				cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

				Expect(hapi.RefreshStatus(ctx, &host, db)).NotTo(HaveOccurred())

				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", host.ID, clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Status).To(Equal(&t.dstState))
			})
		}
	})
	Context("L3 network latency and packet loss validation", func() {

		defaultNTPSourcesInBytes, err := json.Marshal(defaultNTPSources)
		Expect(err).ShouldNot(HaveOccurred())
		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
			hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, nil, defaultConfig, nil, operatorsManager, pr, false, nil)
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
			machineNetworks        []*models.MachineNetwork
			ipType                 int
		}{
			{name: "known with IPv4 and 3 masters",
				srcState:               models.HostStatusDiscovering,
				dstState:               models.HostStatusKnown,
				hostRole:               models.HostRoleMaster,
				latencyInMs:            50,
				packetLossInPercentage: 0,
				IPAddressPool:          hostutil.GenerateIPv4Addresses(2, common.IncrementCidrIP(string(common.TestIPv4Networking.MachineNetworks[0].Cidr))),
				machineNetworks:        common.TestIPv4Networking.MachineNetworks,
				ipType:                 ipv4,
				statusInfoChecker:      makeValueChecker(formatStatusInfoFailedValidation(statusInfoKnown)),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasSufficientNetworkLatencyRequirementForRole: {status: ValidationSuccess, messagePattern: "Network latency requirement has been satisfied."},
					HasSufficientPacketLossRequirementForRole:     {status: ValidationSuccess, messagePattern: "Packet loss requirement has been satisfied."},
				}),
			}, {name: "known with IPv6 and 3 masters",
				srcState:               models.HostStatusDiscovering,
				dstState:               models.HostStatusKnown,
				hostRole:               models.HostRoleMaster,
				latencyInMs:            50,
				packetLossInPercentage: 0,
				IPAddressPool:          hostutil.GenerateIPv6Addresses(2, common.IncrementCidrIP(string(common.TestIPv6Networking.MachineNetworks[0].Cidr))),
				machineNetworks:        common.TestIPv4Networking.MachineNetworks,
				ipType:                 ipv6,
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
				IPAddressPool:          hostutil.GenerateIPv4Addresses(2, common.IncrementCidrIP(string(common.TestIPv4Networking.MachineNetworks[0].Cidr))),
				machineNetworks:        common.TestIPv4Networking.MachineNetworks,
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
				IPAddressPool:          hostutil.GenerateIPv6Addresses(2, common.IncrementCidrIP(string(common.TestIPv6Networking.MachineNetworks[0].Cidr))),
				machineNetworks:        common.TestIPv4Networking.MachineNetworks,
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
				IPAddressPool:          hostutil.GenerateIPv4Addresses(1, common.IncrementCidrIP(string(common.TestIPv6Networking.MachineNetworks[0].Cidr))),
				machineNetworks:        common.TestIPv4Networking.MachineNetworks,
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
				IPAddressPool:          hostutil.GenerateIPv4Addresses(2, common.IncrementCidrIP(string(common.TestIPv4Networking.MachineNetworks[0].Cidr))),
				machineNetworks:        common.TestIPv4Networking.MachineNetworks,
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
				IPAddressPool:          hostutil.GenerateIPv4Addresses(3, common.IncrementCidrIP(string(common.TestIPv4Networking.MachineNetworks[0].Cidr))),
				machineNetworks:        common.TestIPv4Networking.MachineNetworks,
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
				IPAddressPool:          hostutil.GenerateIPv4Addresses(3, common.IncrementCidrIP(string(common.TestIPv4Networking.MachineNetworks[0].Cidr))),
				machineNetworks:        common.TestIPv4Networking.MachineNetworks,
				ipType:                 ipv4,
				statusInfoChecker:      makeRegexChecker("Host cannot be installed due to following failing validation\\(s\\): Network latency requirements of less than or equals 100.00 ms not met for connectivity between.*Packet loss percentage requirement of equals 0.00% not met for connectivity between.*"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasSufficientNetworkLatencyRequirementForRole: {status: ValidationFailure, messagePattern: "Network latency requirements of less than or equals 100.00 ms not met for connectivity between .*? and master-1 \\(200.00 ms\\), master-2 \\(200.00 ms\\)."},
					HasSufficientPacketLossRequirementForRole:     {status: ValidationFailure, messagePattern: "Packet loss percentage requirement of equals 0.00% not met for connectivity between .*? and master-1 \\(1.00%\\), master-2 \\(1.00%\\)."},
				}),
			}, {name: "insufficient with IPv6 and 3 masters with high latency and packet loss",
				srcState:               models.HostStatusDiscovering,
				dstState:               models.HostStatusInsufficient,
				hostRole:               models.HostRoleMaster,
				latencyInMs:            200,
				packetLossInPercentage: 1,
				IPAddressPool:          hostutil.GenerateIPv6Addresses(3, common.IncrementCidrIP(string(common.TestIPv4Networking.MachineNetworks[0].Cidr))),
				machineNetworks:        common.TestIPv4Networking.MachineNetworks,
				ipType:                 ipv6,
				statusInfoChecker:      makeRegexChecker("Host cannot be installed due to following failing validation\\(s\\): Network latency requirements of less than or equals 100.00 ms not met for connectivity between.*Packet loss percentage requirement of equals 0.00% not met for connectivity between.*"),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					HasSufficientNetworkLatencyRequirementForRole: {status: ValidationFailure, messagePattern: "Network latency requirements of less than or equals 100.00 ms not met for connectivity between .*? and master-1 \\(200.00 ms\\), master-2 \\(200.00 ms\\)."},
					HasSufficientPacketLossRequirementForRole:     {status: ValidationFailure, messagePattern: "Packet loss percentage requirement of equals 0.00% not met for connectivity between .*? and master-1 \\(1.00%\\), master-2 \\(1.00%\\)."},
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
					h := hostutil.GenerateTestHostWithNetworkAddress(strfmt.UUID(uuid.New().String()), infraEnvId, clusterId, t.hostRole, t.srcState, netAddr)
					h.NtpSources = string(defaultNTPSourcesInBytes)
					hosts = append(hosts, h)
				}
				cluster = hostutil.GenerateTestClusterWithMachineNetworks(clusterId, t.machineNetworks)
				cluster.UserManagedNetworking = swag.Bool(true)
				cluster.Name = common.TestDefaultConfig.ClusterName
				cluster.BaseDNSDomain = common.TestDefaultConfig.BaseDNSDomain
				connectivityGroups := make(map[string][]strfmt.UUID)
				domainNameResolutions := common.TestDomainNameResolutionsSuccess
				for _, h := range hosts {

					if network.IsIPV4CIDR(string(t.machineNetworks[0].Cidr)) {
						connectivityGroups[network.IPv4.String()] = append(connectivityGroups[network.IPv4.String()], *h.ID)
					} else {
						connectivityGroups[network.IPv6.String()] = append(connectivityGroups[network.IPv6.String()], *h.ID)
					}
					b, err := json.Marshal(domainNameResolutions)
					Expect(err).ShouldNot(HaveOccurred())
					h.DomainNameResolutions = string(b)
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
					Expect(err).ShouldNot(HaveOccurred())
					h.Connectivity = string(b)
					Expect(db.Create(h).Error).ShouldNot(HaveOccurred())
					Expect(err).NotTo(HaveOccurred())
				}
				if t.srcState != t.dstState {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName)))
				}
				Expect(hapi.RefreshStatus(ctx, hosts[0], db)).NotTo(HaveOccurred())

				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hosts[0].ID, clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Status).To(Equal(&t.dstState))
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
		domainNameResolutions := common.TestDomainNameResolutionsSuccess
		Expect(err).ShouldNot(HaveOccurred())
		BeforeEach(func() {
			mockDefaultClusterHostRequirements(mockHwValidator)
			hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, nil, defaultConfig, nil, operatorsManager, pr, false, nil)
			mockDefaultClusterHostRequirements(mockHwValidator)
			cluster = hostutil.GenerateTestCluster(clusterId)
			cluster.UserManagedNetworking = swag.Bool(true)
			cluster.ConnectivityMajorityGroups = fmt.Sprintf("{\"%s\":[\"%s\"]}", network.IPv4.String(), hostId.String())
			cluster.Name = common.TestDefaultConfig.ClusterName
			cluster.BaseDNSDomain = common.TestDefaultConfig.BaseDNSDomain
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

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
					inv, err := common.UnmarshalInventory(t.inventory)
					Expect(err).To(Not(HaveOccurred()))
					inv.Routes = t.routes
					inventory, err = common.MarshalInventory(inv)
					Expect(err).To(Not(HaveOccurred()))
				}
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusDiscovering)
				host.Inventory = inventory
				host.NtpSources = string(defaultNTPSourcesInBytes)
				bytes, err := json.Marshal(domainNameResolutions)
				Expect(err).ShouldNot(HaveOccurred())
				host.DomainNameResolutions = string(bytes)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				if t.srcState != t.dstState {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(hostId.String()),
						eventstest.WithInfraEnvIdMatcher(infraEnvId.String()),
						eventstest.WithClusterIdMatcher(host.ClusterID.String())))
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

	Context("unbound host", func() {

		var (
			infraEnvId strfmt.UUID
			clusterId  strfmt.UUID
		)

		BeforeEach(func() {
			infraEnvId = strfmt.UUID(uuid.New().String())
			clusterId = strfmt.UUID(uuid.New().String())
			infraEnv := &common.InfraEnv{
				InfraEnv: models.InfraEnv{
					ID: &infraEnvId,
				},
			}
			Expect(db.Create(infraEnv).Error).ToNot(HaveOccurred())
		})

		tests := []struct {
			name              string
			srcState          string
			dstState          string
			validCheckInTime  bool
			hostname          string
			inventory         string
			eventRaised       bool
			statusInfoChecker statusInfoChecker
			sourceState       models.SourceState
		}{
			{
				name:              "discovering-unbound to disconnected-unbound",
				srcState:          models.HostStatusDiscoveringUnbound,
				dstState:          models.HostStatusDisconnectedUnbound,
				validCheckInTime:  false,
				eventRaised:       true,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
			},
			{
				name:              "insufficient-unbound to disconnected-unbound",
				srcState:          models.HostStatusInsufficientUnbound,
				dstState:          models.HostStatusDisconnectedUnbound,
				validCheckInTime:  false,
				eventRaised:       true,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
			},
			{
				name:              "known-unbound to disconnected-unbound",
				srcState:          models.HostStatusKnownUnbound,
				dstState:          models.HostStatusDisconnectedUnbound,
				validCheckInTime:  false,
				eventRaised:       true,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
			},
			{
				name:              "disconnected-unbound to disconnected-unbound",
				srcState:          models.HostStatusDisconnectedUnbound,
				dstState:          models.HostStatusDisconnectedUnbound,
				validCheckInTime:  false,
				eventRaised:       false,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
			},
			{
				name:              "discovering-unbound to discovering-unbound",
				srcState:          models.HostStatusDiscoveringUnbound,
				dstState:          models.HostStatusDiscoveringUnbound,
				validCheckInTime:  true,
				inventory:         "",
				eventRaised:       false,
				statusInfoChecker: makeValueChecker(statusInfoDiscovering),
			},
			{
				name:              "disconnected-unbound to discovering-unbound",
				srcState:          models.HostStatusDisconnectedUnbound,
				dstState:          models.HostStatusDiscoveringUnbound,
				validCheckInTime:  true,
				inventory:         "",
				eventRaised:       true,
				statusInfoChecker: makeValueChecker(statusInfoDiscovering),
			},
			{
				name:             "disconnected-unbound to insufficient-unbound",
				srcState:         models.HostStatusDisconnectedUnbound,
				dstState:         models.HostStatusInsufficientUnbound,
				validCheckInTime: true,
				inventory:        insufficientHWInventory(),
				eventRaised:      true,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes",
					"No eligible disks were found, please check specific disks to see why they are not eligible",
					"Require at least 8.00 GiB RAM for role worker, found only 130 bytes",
					"Host couldn't synchronize with any NTP server")),
			},
			{
				name:             "discovering-unbound to insufficient-unbound (invalid hostname)",
				srcState:         models.HostStatusDiscoveringUnbound,
				dstState:         models.HostStatusInsufficientUnbound,
				validCheckInTime: true,
				inventory:        hostutil.GenerateMasterInventoryWithHostname("localhost"),
				eventRaised:      true,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Hostname localhost is forbidden")),
			},
			{
				name:             "discovering-unbound to insufficient-unbound",
				srcState:         models.HostStatusDiscoveringUnbound,
				dstState:         models.HostStatusInsufficientUnbound,
				validCheckInTime: true,
				inventory:        insufficientHWInventory(),
				eventRaised:      true,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes",
					"No eligible disks were found, please check specific disks to see why they are not eligible",
					"Require at least 8.00 GiB RAM for role worker, found only 130 bytes",
					"Host couldn't synchronize with any NTP server")),
			},
			{
				name:             "insufficient-unbound to insufficient-unbound",
				srcState:         models.HostStatusInsufficientUnbound,
				dstState:         models.HostStatusInsufficientUnbound,
				validCheckInTime: true,
				inventory:        insufficientHWInventory(),
				eventRaised:      false,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes",
					"No eligible disks were found, please check specific disks to see why they are not eligible",
					"Require at least 8.00 GiB RAM for role worker, found only 130 bytes",
					"Host couldn't synchronize with any NTP server")),
			},
			{
				name:             "known-unbound to insufficient-unbound",
				srcState:         models.HostStatusKnownUnbound,
				dstState:         models.HostStatusInsufficientUnbound,
				validCheckInTime: true,
				inventory:        insufficientHWInventory(),
				eventRaised:      true,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is 8.00 GiB, found only 130 bytes",
					"No eligible disks were found, please check specific disks to see why they are not eligible",
					"Require at least 8.00 GiB RAM for role worker, found only 130 bytes",
					"Host couldn't synchronize with any NTP server")),
			},
			{
				name:              "binding to binding",
				srcState:          models.HostStatusBinding,
				dstState:          models.HostStatusBinding,
				validCheckInTime:  true,
				inventory:         insufficientHWInventory(),
				eventRaised:       false,
				statusInfoChecker: makeValueChecker(statusInfoBinding),
			},
			{
				name:              "unbinding to unbinding",
				srcState:          models.HostStatusUnbinding,
				dstState:          models.HostStatusUnbinding,
				validCheckInTime:  true,
				inventory:         insufficientHWInventory(),
				eventRaised:       false,
				statusInfoChecker: makeValueChecker(statusInfoUnbinding),
			},
			{
				name:              "disconnected-unbound to known-unbound",
				srcState:          models.HostStatusDisconnectedUnbound,
				dstState:          models.HostStatusKnownUnbound,
				validCheckInTime:  true,
				inventory:         hostutil.GenerateMasterInventoryWithHostname("test-hostname"),
				hostname:          "test-hostname",
				eventRaised:       true,
				statusInfoChecker: makeValueChecker(statusInfoHostReadyToBeBound),
				sourceState:       models.SourceStateSynced,
			},
			{
				name:              "discovering-unbound to known-unbound",
				srcState:          models.HostStatusDiscoveringUnbound,
				dstState:          models.HostStatusKnownUnbound,
				validCheckInTime:  true,
				inventory:         hostutil.GenerateMasterInventoryWithHostname("test-hostname"),
				hostname:          "test-hostname",
				eventRaised:       true,
				statusInfoChecker: makeValueChecker(statusInfoHostReadyToBeBound),
				sourceState:       models.SourceStateSynced,
			},
			{
				name:              "insufficient-unbound to known-unbound",
				srcState:          models.HostStatusInsufficientUnbound,
				dstState:          models.HostStatusKnownUnbound,
				validCheckInTime:  true,
				inventory:         hostutil.GenerateMasterInventoryWithHostname("test-hostname"),
				hostname:          "test-hostname",
				eventRaised:       true,
				statusInfoChecker: makeValueChecker(statusInfoHostReadyToBeBound),
				sourceState:       models.SourceStateSynced,
			},
			{
				name:              "known-unbound to known-unbound",
				srcState:          models.HostStatusKnownUnbound,
				dstState:          models.HostStatusKnownUnbound,
				validCheckInTime:  true,
				inventory:         hostutil.GenerateMasterInventory(),
				eventRaised:       false,
				statusInfoChecker: makeValueChecker(statusInfoHostReadyToBeBound),
				sourceState:       models.SourceStateSynced,
			},
			{
				name:              "disconnected-unbound to known-unbound - no NTP",
				srcState:          models.HostStatusDisconnectedUnbound,
				dstState:          models.HostStatusKnownUnbound,
				validCheckInTime:  true,
				inventory:         hostutil.GenerateMasterInventoryWithHostname("test-hostname"),
				hostname:          "test-hostname",
				eventRaised:       true,
				statusInfoChecker: makeValueChecker(statusInfoHostReadyToBeBound),
			},
			{
				name:              "discovering-unbound to known-unbound - no NTP",
				srcState:          models.HostStatusDiscoveringUnbound,
				dstState:          models.HostStatusKnownUnbound,
				validCheckInTime:  true,
				inventory:         hostutil.GenerateMasterInventoryWithHostname("test-hostname"),
				hostname:          "test-hostname",
				eventRaised:       true,
				statusInfoChecker: makeValueChecker(statusInfoHostReadyToBeBound),
			},
			{
				name:              "insufficient-unbound to known-unbound - no NTP",
				srcState:          models.HostStatusInsufficientUnbound,
				dstState:          models.HostStatusKnownUnbound,
				validCheckInTime:  true,
				inventory:         hostutil.GenerateMasterInventoryWithHostname("test-hostname"),
				hostname:          "test-hostname",
				eventRaised:       true,
				statusInfoChecker: makeValueChecker(statusInfoHostReadyToBeBound),
			},
			{
				name:              "known-unbound to known-unbound - no NTP",
				srcState:          models.HostStatusKnownUnbound,
				dstState:          models.HostStatusKnownUnbound,
				validCheckInTime:  true,
				inventory:         hostutil.GenerateMasterInventory(),
				eventRaised:       false,
				statusInfoChecker: makeValueChecker(statusInfoHostReadyToBeBound),
			},
			{
				name:             "disconnected-unbound to insufficient-unbound un-synced NTP",
				srcState:         models.HostStatusDisconnectedUnbound,
				dstState:         models.HostStatusInsufficientUnbound,
				validCheckInTime: true,
				inventory:        hostutil.GenerateMasterInventoryWithHostname("test-hostname"),
				hostname:         "test-hostname",
				eventRaised:      true,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Host couldn't synchronize with any NTP server")),
				sourceState: models.SourceStateUnreachable,
			},
			{
				name:             "discovering-unbound to insufficient-unbound un-synced NTP",
				srcState:         models.HostStatusDiscoveringUnbound,
				dstState:         models.HostStatusInsufficientUnbound,
				validCheckInTime: true,
				inventory:        hostutil.GenerateMasterInventoryWithHostname("test-hostname"),
				hostname:         "test-hostname",
				eventRaised:      true,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Host couldn't synchronize with any NTP server")),
				sourceState: models.SourceStateUnreachable,
			},
			{
				name:             "insufficient-unbound to insufficient-unbound un-synced NTP",
				srcState:         models.HostStatusInsufficientUnbound,
				dstState:         models.HostStatusInsufficientUnbound,
				validCheckInTime: true,
				inventory:        hostutil.GenerateMasterInventoryWithHostname("test-hostname"),
				hostname:         "test-hostname",
				eventRaised:      false,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Host couldn't synchronize with any NTP server")),
				sourceState: models.SourceStateUnreachable,
			},
			{
				name:             "known-unbound to insufficient-unbound un-synced NTP",
				srcState:         models.HostStatusKnownUnbound,
				dstState:         models.HostStatusInsufficientUnbound,
				validCheckInTime: true,
				hostname:         "master-hostname",
				inventory:        hostutil.GenerateMasterInventory(),
				eventRaised:      true,
				statusInfoChecker: makeValueChecker(formatStatusInfoFailedValidation(statusInfoInsufficientHardware,
					"Host couldn't synchronize with any NTP server")),
				sourceState: models.SourceStateUnreachable,
			},
			{
				name:              "reclaiming to reclaiming",
				srcState:          models.HostStatusReclaiming,
				dstState:          models.HostStatusReclaiming,
				validCheckInTime:  true,
				inventory:         hostutil.GenerateMasterInventory(),
				eventRaised:       false,
				statusInfoChecker: makeValueChecker(statusInfoUnbinding),
				sourceState:       models.SourceStateSynced,
			},
			{
				name:              "reclaiming-rebooting to reclaiming-rebooting",
				srcState:          models.HostStatusReclaimingRebooting,
				dstState:          models.HostStatusReclaimingRebooting,
				validCheckInTime:  true,
				inventory:         hostutil.GenerateMasterInventory(),
				eventRaised:       false,
				statusInfoChecker: makeValueChecker(statusInfoRebootingForReclaim),
				sourceState:       models.SourceStateSynced,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				hostCheckInAt := strfmt.DateTime(time.Now())
				host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, t.srcState)
				host.Inventory = t.inventory
				host.ClusterID = nil
				if !t.validCheckInTime {
					// Timeout for checkin is 3 minutes so subtract 4 minutes from the current time
					hostCheckInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
				}
				host.CheckedInAt = hostCheckInAt
				if t.sourceState != "" {
					b, err := json.Marshal(&[]*models.NtpSource{
						{
							SourceName:  "source",
							SourceState: t.sourceState,
						},
					})
					Expect(err).ToNot(HaveOccurred())
					host.NtpSources = string(b)
					Expect(db.Model(&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvId}}).Update("additional_ntp_sources", "source").Error).ToNot(HaveOccurred())
				}
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

				mockDefaultInfraEnvHostRequirements(mockHwValidator)
				if t.eventRaised {
					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
						eventstest.WithHostIdMatcher(hostId.String()),
						eventstest.WithInfraEnvIdMatcher(infraEnvId.String()),
						eventstest.WithClusterIdMatcher(swag.StringValue(nil))))
				}

				Expect(hapi.RefreshStatus(ctx, &host, db)).NotTo(HaveOccurred())
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and infra_env_id = ?", host.ID, infraEnvId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Status).To(Equal(&t.dstState))
			})
		}
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

var _ = Describe("Comparison builder", func() {
	It("should return 'equals' when value is == 0", func() {
		Expect(comparisonBuilder(0)).To(Equal("equals"))
	})
	It("should return 'less than or equals' when value is > 0", func() {
		Expect(comparisonBuilder(1)).To(Equal("less than or equals"))
	})
})

var _ = Describe("Upgrade agent feature", func() {
	var (
		ctx                           = context.Background()
		hapi                          API
		db                            *gorm.DB
		ctrl                          *gomock.Controller
		mockEvents                    *eventsapi.MockHandler
		hostId, clusterId, infraEnvId strfmt.UUID
		dbName                        string
		mockHwValidator               *hardware.MockValidator
		pr                            *registry.MockProviderRegistry
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		operatorsManager := operators.NewManager(
			common.GetTestLog(),
			nil,
			operators.Options{},
			nil,
			nil,
		)
		pr = registry.NewMockProviderRegistry(ctrl)
		hapi = NewManager(
			common.GetTestLog(),
			db,
			mockEvents,
			mockHwValidator,
			nil,
			createValidatorCfg(),
			nil,
			defaultConfig,
			nil,
			operatorsManager,
			pr,
			false,
			nil,
		)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("Moves from known to insufficient when agent isn't compatible", func() {
		// Create the infrastructure environment:
		infraEnv := hostutil.GenerateTestInfraEnv(infraEnvId)
		Expect(db.Create(&infraEnv).Error).ToNot(HaveOccurred())

		// Create the cluster:
		cluster := hostutil.GenerateTestCluster(clusterId)
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		// Create the host:
		host := hostutil.GenerateTestHost(
			hostId,
			infraEnvId,
			clusterId,
			models.HostStatusKnown,
		)
		host.RequestedHostname = "my-host"
		host.NtpSources = `[{
			"source_name": "my.ntp.org",
			"source_state": "synced"
		}]`
		host.DiscoveryAgentVersion = "quay.io/my/image:bad"
		Expect(db.Create(&host).Error).ToNot(HaveOccurred())

		// Prepare the mocks:
		mockDefaultClusterHostRequirements(mockHwValidator)
		mockPreflightHardwareRequirements(
			mockHwValidator,
			&defaultMasterRequirements,
			&defaultWorkerRequirements,
		)
		disks := []*models.Disk{{
			ID:     "/dev/disks/by-id/123",
			Name:   "my-disk",
			Serial: "123",
			InstallationEligibility: models.DiskInstallationEligibility{
				Eligible: true,
			},
		}}
		mockHwValidator.EXPECT().ListEligibleDisks(gomock.Any()).
			Return(disks).AnyTimes()
		mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).
			Return("/dev/sda").AnyTimes()
		pr.EXPECT().IsHostSupported(gomock.Any(), gomock.Any()).
			Return(true, nil).AnyTimes()
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), gomock.Any()).
			AnyTimes()

		// Refresh the host:
		err := hapi.RefreshStatus(ctx, &host, db)
		Expect(err).ToNot(HaveOccurred())

		// Check that the agent compatibility validation fails and that all the other
		// succeed, as otherwise the host may be moved to the insufficent state because of
		// some other validation and then the result of the test will be a false positive.
		host = hostutil.GetHostFromDB(hostId, infraEnvId, db).Host
		validations, err := GetValidations(&host)
		Expect(err).ToNot(HaveOccurred())
		for _, results := range validations {
			for _, result := range results {
				if result.ID == CompatibleAgent {
					Expect(result.Status).To(
						Equal(ValidationFailure),
						"Expected compatible agent validation to fail, "+
							"but it didn't",
					)
				} else {
					Expect(result.Status).ToNot(
						Equal(ValidationFailure),
						"Expected '%s' validation to not fail, but it did",
						result.ID,
					)
				}
			}
		}

		// Check that it is moved to the insufficient state:
		Expect(host.Status).To(HaveValue(Equal(models.HostStatusInsufficient)))
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

func mockDefaultInfraEnvHostRequirements(mockHwValidator *hardware.MockValidator) {
	mockHwValidator.EXPECT().GetInfraEnvHostRequirements(gomock.Any(), gomock.Any()).Times(1).DoAndReturn(func(ctx context.Context, infraEnv *common.InfraEnv) (*models.ClusterHostRequirements, error) {
		details := defaultWorkerRequirements
		return &models.ClusterHostRequirements{Total: &details}, nil
	})

	mockPreflightInfraEnvHardwareRequirements(mockHwValidator, &defaultMasterRequirements, &defaultWorkerRequirements)
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

func mockPreflightInfraEnvHardwareRequirements(mockHwValidator *hardware.MockValidator, masterRequirements, workerRequirements *models.ClusterHostRequirementsDetails) *gomock.Call {
	return mockHwValidator.EXPECT().GetPreflightInfraEnvHardwareRequirements(gomock.Any(), gomock.Any()).Times(1).Return(
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

func formatProgressTimedOutInfo(config *Config, stage models.HostStage) string {
	timeFormat := config.HostStageTimeout(stage).String()
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

func generateMajorityGroup(machineNetworks []*models.MachineNetwork, hostId strfmt.UUID) string {
	majorityGroups := map[string][]string{}
	for _, net := range machineNetworks {
		majorityGroups[string(net.Cidr)] = append(majorityGroups[string(net.Cidr)], hostId.String())
	}
	tmp, err := json.Marshal(majorityGroups)
	if err != nil {
		return ""
	}
	return string(tmp)
}

func addL3Connectivity(majorityGroupsStr string, hostId strfmt.UUID) string {
	majorityGroups := make(map[string][]string)
	if majorityGroupsStr != "" {
		Expect(json.Unmarshal([]byte(majorityGroupsStr), &majorityGroups)).ToNot(HaveOccurred())
	}
	majorityGroups[network.IPv4.String()] = []string{hostId.String()}
	tmp, err := json.Marshal(majorityGroups)
	Expect(err).ToNot(HaveOccurred())
	return string(tmp)
}

func validateEqualProgress(p1, p2 *models.HostProgressInfo) {
	if p1 == nil {
		Expect(p2).To(BeNil())
	} else {
		Expect(p1.CurrentStage).To(Equal(p2.CurrentStage))
		Expect(p1.InstallationPercentage).To(Equal(p2.InstallationPercentage))
		Expect(p1.ProgressInfo).To(Equal(p2.ProgressInfo))
		Expect(time.Time(p1.StageStartedAt).Equal(time.Time(p2.StageStartedAt))).To(BeTrue())
		Expect(time.Time(p1.StageUpdatedAt).Equal(time.Time(p2.StageUpdatedAt))).To(BeTrue())
	}
}

type testState struct {
	state string
}

func newTestState(state string) *testState {
	return &testState{
		state: state,
	}
}

func (ts *testState) State() stateswitch.State {
	return stateswitch.State(ts.state)
}

func (ts *testState) SetState(state stateswitch.State) error {
	ts.state = string(state)
	return nil
}

var allValidationIDs []validationID = []validationID{
	IsMediaConnected,
	IsConnected,
	HasInventory,
	IsMachineCidrDefined,
	BelongsToMachineCidr,
	HasMinCPUCores,
	HasMinValidDisks,
	HasMinMemory,
	HasCPUCoresForRole,
	HasMemoryForRole,
	IsHostnameUnique,
	IsHostnameValid,
	IsIgnitionDownloadable,
	BelongsToMajorityGroup,
	IsPlatformNetworkSettingsValid,
	IsNTPSynced,
	IsTimeSyncedBetweenHostAndService,
	SucessfullOrUnknownContainerImagesAvailability,
	AreLsoRequirementsSatisfied,
	AreOdfRequirementsSatisfied,
	AreCnvRequirementsSatisfied,
	AreLvmRequirementsSatisfied,
	SufficientOrUnknownInstallationDiskSpeed,
	HasSufficientNetworkLatencyRequirementForRole,
	HasSufficientPacketLossRequirementForRole,
	HasDefaultRoute,
	IsAPIDomainNameResolvedCorrectly,
	IsAPIInternalDomainNameResolvedCorrectly,
	IsAppsDomainNameResolvedCorrectly,
	CompatibleWithClusterPlatform,
	IsDNSWildcardNotConfigured,
	DiskEncryptionRequirementsSatisfied,
	NonOverlappingSubnets,
	VSphereHostUUIDEnabled,
	CompatibleAgent,
	NoSkipInstallationDisk,
	NoSkipMissingDisk,
}

var allConditions []conditionId = []conditionId{
	InstallationDiskSpeedCheckSuccessful,
	ClusterPreparingForInstallation,
	ClusterPendingUserAction,
	ClusterInstalling,
	ValidRoleForInstallation,
	StageInWrongBootStages,
	ClusterInError,
	SuccessfulContainerImageAvailability,
}

var knownStateConditions map[string]bool

func initKnownStateConditions() {
	knownStateConditions = make(map[string]bool)
	for _, condition := range allConditions {
		knownStateConditions[string(condition)] = false
	}

	knownStateConditions[string(ValidRoleForInstallation)] = true
}

func init() {
	initKnownStateConditions()
}

func initializeRefreshHostArgs(validationIDs []validationID, status ValidationStatus, conditions map[string]bool) TransitionArgsRefreshHost {
	refreshHostArgs := TransitionArgsRefreshHost{
		ctx:               context.Background(),
		eventHandler:      nil,
		conditions:        conditions,
		validationResults: map[string]ValidationResults{},
		db:                &gorm.DB{},
	}

	for _, validationID := range validationIDs {
		validationCategory, err := validationID.category()
		Expect(err).ToNot(HaveOccurred())
		refreshHostArgs.validationResults[validationCategory] = append(
			refreshHostArgs.validationResults[validationCategory], ValidationResult{
				ID:      validationID,
				Status:  status,
				Message: "Test validation",
			})
		refreshHostArgs.conditions[string(validationID)] = true
	}

	return refreshHostArgs
}

var _ = Describe("State machine test - refresh transition", func() {
	var (
		mockController        *gomock.Controller
		mockTransitionHandler *MockTransitionHandler
		stateMachine          stateswitch.StateMachine
		refreshHostArgs       TransitionArgsRefreshHost
		testState             *testState
	)

	// Initializes mocks needed for [NewHostStateMachine]
	newStateMachineMocks := func(mockTransitionHandler *MockTransitionHandler) {
		mockTransitionHandler.EXPECT().HasStatusTimedOut(gomock.Any()).Return(func(_ stateswitch.StateSwitch, _ stateswitch.TransitionArgs) (bool, error) {
			return false, nil
		}).AnyTimes()

		mockTransitionHandler.EXPECT().PostRefreshHost(gomock.Any()).Return(
			func(_ stateswitch.StateSwitch, _ stateswitch.TransitionArgs) error { return nil },
		).AnyTimes()

		mockTransitionHandler.EXPECT().PostRefreshLogsProgress(gomock.Any()).Return(
			func(_ stateswitch.StateSwitch, _ stateswitch.TransitionArgs) error { return nil },
		).AnyTimes()
	}

	BeforeEach(func() {
		mockController = gomock.NewController(GinkgoT())
		mockTransitionHandler = NewMockTransitionHandler(mockController)

		// Initialize the state machine
		newStateMachineMocks(mockTransitionHandler)
		stateMachine = NewHostStateMachine(stateswitch.NewStateMachine(), mockTransitionHandler)
	})

	AfterEach(func() {
		mockController.Finish()
	})

	Context("Host in known state", func() {
		testState = newTestState(models.HostStatusKnown)

		BeforeEach(func() {
			mockTransitionHandler.EXPECT().PostPreparingForInstallationHost(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

			refreshHostArgs = initializeRefreshHostArgs(allValidationIDs, ValidationSuccess, knownStateConditions)

			// Ensure we've actually set up the validations correctly by
			// running a refresh and checking that we're still in Known
			Expect(stateMachine.Run(TransitionTypeRefresh, testState, &refreshHostArgs)).To(Succeed())
			Expect(string(testState.State())).To(Equal(models.HostStatusKnown))
		})

		It("Moves from known to insufficient when disk skip validations fail - no skip installation disk", func() {
			refreshHostArgs.conditions[string(NoSkipInstallationDisk)] = false

			Expect(stateMachine.Run(TransitionTypeRefresh, testState, &refreshHostArgs)).To(Succeed())
			Expect(string(testState.State())).To(Equal(models.HostStatusInsufficient))

			refreshHostArgs.conditions[string(NoSkipInstallationDisk)] = true

			Expect(stateMachine.Run(TransitionTypeRefresh, testState, &refreshHostArgs)).To(Succeed())
			Expect(string(testState.State())).To(Equal(models.HostStatusKnown))
		})

		It("Moves from known to insufficient when disk skip validations fail - no skip missing disk", func() {
			refreshHostArgs.conditions[string(NoSkipMissingDisk)] = false

			Expect(stateMachine.Run(TransitionTypeRefresh, testState, &refreshHostArgs)).To(Succeed())
			Expect(string(testState.State())).To(Equal(models.HostStatusInsufficient))

			refreshHostArgs.conditions[string(NoSkipMissingDisk)] = true

			Expect(stateMachine.Run(TransitionTypeRefresh, testState, &refreshHostArgs)).To(Succeed())
			Expect(string(testState.State())).To(Equal(models.HostStatusKnown))
		})
	})

})
