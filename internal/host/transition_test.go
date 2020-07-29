package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/internal/events"
	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/internal/metrics"
	"github.com/filanov/bm-inventory/models"

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
		MinCPUCores:       2,
		MinCPUCoresWorker: 2,
		MinCPUCoresMaster: 4,
		MinDiskSizeGb:     120,
		MinRamGib:         8,
		MinRamGibWorker:   8,
		MinRamGibMaster:   16,
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
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	It("register_new", func() {
		Expect(hapi.RegisterHost(ctx, &models.Host{ID: &hostId, ClusterID: clusterId, DiscoveryAgentVersion: "v1.0.1"})).ShouldNot(HaveOccurred())
		h := getHost(hostId, clusterId, db)
		Expect(swag.StringValue(h.Status)).Should(Equal(HostStatusDiscovering))
		Expect(h.DiscoveryAgentVersion).To(Equal("v1.0.1"))
	})

	Context("register during installation put host in error", func() {
		tests := []struct {
			name     string
			srcState string
		}{
			{
				name:     "discovering",
				srcState: HostStatusInstalling,
			},
			{
				name:     "insufficient",
				srcState: HostStatusInstallingInProgress,
			},
		}

		AfterEach(func() {
			h := getHost(hostId, clusterId, db)
			Expect(swag.StringValue(h.Status)).Should(Equal(HostStatusError))
			Expect(h.Role).Should(Equal(models.HostRoleMaster))
			Expect(h.Inventory).Should(Equal(defaultHwInfo))
			Expect(h.StatusInfo).NotTo(BeNil())
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
				mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), models.EventSeverityError,
					fmt.Sprintf("Host %s: updated status from \"%s\" to \"error\" (The host unexpectedly restarted during the installation)", hostId.String(), t.srcState),
					gomock.Any(), clusterId.String())

				Expect(hapi.RegisterHost(ctx, &models.Host{
					ID:        &hostId,
					ClusterID: clusterId,
					Status:    swag.String(t.srcState),
				})).ShouldNot(HaveOccurred())
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
				srcState: HostStatusDiscovering,
			},
			{
				name:     "insufficient",
				srcState: HostStatusInsufficient,
			},
			{
				name:     "disconnected",
				srcState: HostStatusDisconnected,
			},
			{
				name:     "known",
				srcState: HostStatusKnown,
			},
		}

		AfterEach(func() {
			h := getHost(hostId, clusterId, db)
			Expect(swag.StringValue(h.Status)).Should(Equal(HostStatusDiscovering))
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
				mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), models.EventSeverityInfo,
					fmt.Sprintf("Host %s: updated status from \"%s\" to \"discovering\" (Waiting for host hardware info)", hostId.String(), t.srcState),
					gomock.Any(), clusterId.String())

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
				srcState: HostStatusDisabled,
			},
			{
				name:     "error",
				srcState: HostStatusError,
			},
			{
				name:     "installed",
				srcState: HostStatusInstalled,
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
			name     string
			srcState string
			progress models.HostProgressInfo
		}{
			{
				name:     "host in reboot",
				srcState: HostStatusInstallingInProgress,
				progress: models.HostProgressInfo{
					CurrentStage: models.HostStageRebooting,
				},
			},
		}

		AfterEach(func() {
			h := getHost(hostId, clusterId, db)
			Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusInstallingPendingUserAction))
			Expect(h.Role).Should(Equal(models.HostRoleMaster))
			Expect(h.Inventory).Should(Equal(defaultHwInfo))
			Expect(h.StatusInfo).NotTo(BeNil())
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
					Progress:  &t.progress,
				}).Error).ShouldNot(HaveOccurred())
				mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), models.EventSeverityWarning,
					fmt.Sprintf("Host %s: updated status from \"installing-in-progress\" to \"installing-pending-user-action\" "+
						"(Expected the host to boot from disk, but it booted the installation image - please reboot and fix boot order "+
						"to boot from disk)", hostId.String()),
					gomock.Any(), clusterId.String())

				Expect(hapi.RegisterHost(ctx, &models.Host{
					ID:        &hostId,
					ClusterID: clusterId,
					Status:    swag.String(t.srcState),
				})).ShouldNot(HaveOccurred())
			})
		}
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
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
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), mockMetric)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(hostId, clusterId, "")
		host.Status = swag.String(HostStatusInstalling)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("handle_installation_error", func() {
		mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), models.EventSeverityError,
			fmt.Sprintf("Host %s: updated status from \"installing\" to \"error\" (installation command failed)", host.ID.String()),
			gomock.Any(), host.ClusterID.String())
		mockMetric.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
		Expect(hapi.HandleInstallationFailure(ctx, &host)).ShouldNot(HaveOccurred())
		h := getHost(hostId, clusterId, db)
		Expect(swag.StringValue(h.Status)).Should(Equal(HostStatusError))
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
		hapi = NewManager(getTestLog(), db, mockEventsHandler, nil, nil, createValidatorCfg(), nil)
	})

	tests := []struct {
		state      string
		success    bool
		statusCode int32
	}{
		{state: models.HostStatusInstalling, success: true},
		{state: models.HostStatusInstallingInProgress, success: true},
		{state: models.HostStatusInstalled, success: true},
		{state: models.HostStatusError, success: true},
		{state: models.HostStatusDisabled, success: true},
		{state: models.HostStatusDiscovering, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusKnown, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusPreparingForInstallation, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusPendingForInput, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusInstallingPendingUserAction, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusResettingPendingUserAction, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusDisconnected, success: false, statusCode: http.StatusConflict},
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
				Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusResetting))
			} else {
				Expect(err).Should(HaveOccurred())
				Expect(err.StatusCode()).Should(Equal(t.statusCode))
				Expect(swag.StringValue(h.Status)).Should(Equal(t.state))
			}
		})
	}

	AfterEach(func() {
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
		hapi = NewManager(getTestLog(), db, mockEventsHandler, nil, nil, createValidatorCfg(), nil)
	})

	tests := []struct {
		state      string
		success    bool
		statusCode int32
	}{
		{state: models.HostStatusInstalling, success: true},
		{state: models.HostStatusInstallingInProgress, success: true},
		{state: models.HostStatusInstalled, success: true},
		{state: models.HostStatusError, success: true},
		{state: models.HostStatusDisabled, success: true},
		{state: models.HostStatusDiscovering, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusKnown, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusPreparingForInstallation, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusPendingForInput, success: false, statusCode: http.StatusConflict},
		{state: models.HostStatusInstallingPendingUserAction, success: false, statusCode: http.StatusConflict},
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
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("install host", func() {
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(HostStatusInstalling))
			Expect(*h.StatusInfo).Should(Equal(statusInfoInstalling))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
		}

		noChange := func(reply error) {
			Expect(reply).To(BeNil())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(HostStatusDisabled))
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
				srcState:   HostStatusKnown,
				validation: failure,
			},
			{
				name:       "disabled nothing change",
				srcState:   HostStatusDisabled,
				validation: noChange,
			},
			{
				name:       "disconnected",
				srcState:   HostStatusDisconnected,
				validation: failure,
			},
			{
				name:       "discovering",
				srcState:   HostStatusDiscovering,
				validation: failure,
			},
			{
				name:       "error",
				srcState:   HostStatusError,
				validation: failure,
			},
			{
				name:       "installed",
				srcState:   HostStatusInstalled,
				validation: failure,
			},
			{
				name:       "installing",
				srcState:   HostStatusInstalling,
				validation: failure,
			},
			{
				name:       "in-progress",
				srcState:   HostStatusInstallingInProgress,
				validation: failure,
			},
			{
				name:       "insufficient",
				srcState:   HostStatusInsufficient,
				validation: failure,
			},
			{
				name:       "resetting",
				srcState:   HostStatusResetting,
				validation: failure,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				host = getTestHost(hostId, clusterId, t.srcState)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), models.EventSeverityInfo,
					fmt.Sprintf("Host %s: updated status from \"%s\" to \"installing\" (Installation in progress)", host.ID.String(), t.srcState),
					gomock.Any(), host.ClusterID.String())
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
			mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), models.EventSeverityInfo,
				fmt.Sprintf("Host %s: updated status from \"preparing-for-installation\" to \"installing\" (Installation in progress)", host.ID.String()),
				gomock.Any(), host.ClusterID.String())
			Expect(hapi.Install(ctx, &host, tx)).ShouldNot(HaveOccurred())
			Expect(tx.Commit().Error).ShouldNot(HaveOccurred())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(HostStatusInstalling))
			Expect(*h.StatusInfo).Should(Equal(statusInfoInstalling))
		})

		It("rollback transition", func() {
			tx := db.Begin()
			Expect(tx.Error).To(BeNil())
			mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), models.EventSeverityInfo,
				fmt.Sprintf("Host %s: updated status from \"preparing-for-installation\" to \"installing\" (Installation in progress)", host.ID.String()),
				gomock.Any(), host.ClusterID.String())
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
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("disable host", func() {
		var srcState string
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(HostStatusDisabled))
			Expect(*h.StatusInfo).Should(Equal(statusInfoDisabled))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(srcState))
		}

		tests := []struct {
			name       string
			srcState   string
			validation func(error)
		}{
			{
				name:       "known",
				srcState:   HostStatusKnown,
				validation: success,
			},
			{
				name:       "disabled nothing change",
				srcState:   HostStatusDisabled,
				validation: failure,
			},
			{
				name:       "disconnected",
				srcState:   HostStatusDisconnected,
				validation: success,
			},
			{
				name:       "discovering",
				srcState:   HostStatusDiscovering,
				validation: success,
			},
			{
				name:       "error",
				srcState:   HostStatusError,
				validation: failure,
			},
			{
				name:       "installed",
				srcState:   HostStatusInstalled,
				validation: failure,
			},
			{
				name:       "installing",
				srcState:   HostStatusInstalling,
				validation: failure,
			},
			{
				name:       "in-progress",
				srcState:   HostStatusInstallingInProgress,
				validation: failure,
			},
			{
				name:       "insufficient",
				srcState:   HostStatusInsufficient,
				validation: success,
			},
			{
				name:       "resetting",
				srcState:   HostStatusResetting,
				validation: failure,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				srcState = t.srcState
				host = getTestHost(hostId, clusterId, srcState)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), models.EventSeverityInfo,
					fmt.Sprintf("Host %s: updated status from \"%s\" to \"disabled\" (Host is disabled)", host.ID.String(), t.srcState),
					gomock.Any(), host.ClusterID.String())
				t.validation(hapi.DisableHost(ctx, &host))
			})
		}
	})

	AfterEach(func() {
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
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("enable host", func() {
		var srcState string
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(HostStatusDiscovering))
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
				srcState:   HostStatusKnown,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "disabled to enable",
				srcState:   HostStatusDisabled,
				validation: success,
				sendEvent:  true,
			},
			{
				name:       "disconnected",
				srcState:   HostStatusDisconnected,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "discovering",
				srcState:   HostStatusDiscovering,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "error",
				srcState:   HostStatusError,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "installed",
				srcState:   HostStatusInstalled,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "installing",
				srcState:   HostStatusInstalling,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "in-progress",
				srcState:   HostStatusInstallingInProgress,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "insufficient",
				srcState:   HostStatusInsufficient,
				validation: failure,
				sendEvent:  false,
			},
			{
				name:       "resetting",
				srcState:   HostStatusResetting,
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
					mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), models.EventSeverityInfo,
						fmt.Sprintf("Host %s: updated status from \"%s\" to \"discovering\" (Waiting for host hardware info)", common.GetHostnameForMsg(&host), srcState),
						gomock.Any(), host.ClusterID.String())
				}
				t.validation(hapi.EnableHost(ctx, &host))
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
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})
	Context("All transitions", func() {
		var srcState string
		tests := []struct {
			name               string
			srcState           string
			inventory          string
			role               string
			machineNetworkCidr string
			checkedInAt        strfmt.DateTime
			dstState           string
			statusInfoChecker  statusInfoChecker
			validationsChecker *validationsChecker
			errorExpected      bool
		}{
			{
				name:              "discovering to disconnected",
				srcState:          HostStatusDiscovering,
				dstState:          HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationFailure, messagePattern: "Host is disconnected"},
					HasInventory:         {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:       {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:         {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:     {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					IsRoleDefined:        {status: ValidationFailure, messagePattern: "Role is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "insufficient to disconnected",
				srcState:          HostStatusInsufficient,
				dstState:          HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationFailure, messagePattern: "Host is disconnected"},
					HasInventory:         {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:       {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:         {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:     {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					IsRoleDefined:        {status: ValidationFailure, messagePattern: "Role is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "known to disconnected",
				srcState:          HostStatusKnown,
				dstState:          HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				errorExpected:     false,
			},
			{
				name:              "pending to disconnected",
				srcState:          HostStatusPendingForInput,
				dstState:          HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationFailure, messagePattern: "Host is disconnected"},
					HasInventory:         {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:       {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:         {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:     {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					IsRoleDefined:        {status: ValidationFailure, messagePattern: "Role is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "disconnected to disconnected",
				srcState:          HostStatusDisconnected,
				dstState:          HostStatusDisconnected,
				statusInfoChecker: makeValueChecker(statusInfoDisconnected),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationFailure, messagePattern: "Host is disconnected"},
					HasInventory:         {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:       {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:         {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:     {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					IsRoleDefined:        {status: ValidationFailure, messagePattern: "Role is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "disconnected to discovering",
				checkedInAt:       strfmt.DateTime(time.Now()),
				srcState:          HostStatusDisconnected,
				dstState:          HostStatusDiscovering,
				statusInfoChecker: makeValueChecker(statusInfoDiscovering),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:       {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:         {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:     {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					IsRoleDefined:        {status: ValidationFailure, messagePattern: "Role is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "discovering to discovering",
				checkedInAt:       strfmt.DateTime(time.Now()),
				srcState:          HostStatusDiscovering,
				dstState:          HostStatusDiscovering,
				statusInfoChecker: makeValueChecker(statusInfoDiscovering),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationFailure, messagePattern: "Inventory has not been received for the host"},
					HasMinCPUCores:       {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinMemory:         {status: ValidationPending, messagePattern: "Missing inventory"},
					HasMinValidDisks:     {status: ValidationPending, messagePattern: "Missing inventory"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					IsRoleDefined:        {status: ValidationFailure, messagePattern: "Role is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationPending, messagePattern: "Missing inventory"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "disconnected to insufficient (1)",
				checkedInAt:       strfmt.DateTime(time.Now()),
				srcState:          HostStatusDisconnected,
				dstState:          HostStatusInsufficient,
				statusInfoChecker: makeValueChecker(statusInfoInsufficientHardware),
				inventory:         insufficientHWInventory(),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "Require at least 8 GiB RAM, found only 0 GiB"},
					HasMinValidDisks:     {status: ValidationFailure, messagePattern: "Require a disk of at least 120 GB"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					IsRoleDefined:        {status: ValidationFailure, messagePattern: "Role is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:              "insufficient to insufficient (1)",
				checkedInAt:       strfmt.DateTime(time.Now()),
				srcState:          HostStatusInsufficient,
				dstState:          HostStatusInsufficient,
				statusInfoChecker: makeValueChecker(statusInfoInsufficientHardware),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "Require at least 8 GiB RAM, found only 0 GiB"},
					HasMinValidDisks:     {status: ValidationFailure, messagePattern: "Require a disk of at least 120 GB"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					IsRoleDefined:        {status: ValidationFailure, messagePattern: "Role is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				inventory:     insufficientHWInventory(),
				errorExpected: false,
			},
			{
				name:              "discovering to insufficient (1)",
				checkedInAt:       strfmt.DateTime(time.Now()),
				srcState:          HostStatusDiscovering,
				dstState:          HostStatusInsufficient,
				statusInfoChecker: makeValueChecker(statusInfoInsufficientHardware),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationFailure, messagePattern: "Require at least 8 GiB RAM, found only 0 GiB"},
					HasMinValidDisks:     {status: ValidationFailure, messagePattern: "Require a disk of at least 120 GB"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					IsRoleDefined:        {status: ValidationFailure, messagePattern: "Role is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				inventory:     insufficientHWInventory(),
				errorExpected: false,
			},
			{
				name:              "pending to insufficient (1)",
				checkedInAt:       strfmt.DateTime(time.Now()),
				srcState:          HostStatusPendingForInput,
				dstState:          HostStatusPendingForInput,
				statusInfoChecker: makeValueChecker(""),
				inventory:         insufficientHWInventory(),
				errorExpected:     true,
			},
			{
				name:              "known to insufficient (1)",
				checkedInAt:       strfmt.DateTime(time.Now()),
				srcState:          HostStatusKnown,
				dstState:          HostStatusKnown,
				statusInfoChecker: makeValueChecker(""),
				inventory:         insufficientHWInventory(),
				errorExpected:     true,
			},
			{
				name:              "disconnected to pending",
				checkedInAt:       strfmt.DateTime(time.Now()),
				srcState:          HostStatusDisconnected,
				dstState:          HostStatusPendingForInput,
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					IsRoleDefined:        {status: ValidationFailure, messagePattern: "Role is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationPending, messagePattern: "Missing inventory or machine network CIDR"},
				}),
				inventory:     workerInventory(),
				errorExpected: false,
			},
			{
				name:               "discovering to pending",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusDiscovering,
				dstState:           HostStatusPendingForInput,
				machineNetworkCidr: "5.6.7.0/24",
				statusInfoChecker:  makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationFailure, messagePattern: "Role is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationFailure, messagePattern: "Host does not belong to machine network CIDR"},
				}),
				inventory:     workerInventory(),
				errorExpected: false,
			},
			{
				name:               "insufficient to pending",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusInsufficient,
				dstState:           HostStatusPendingForInput,
				machineNetworkCidr: "5.6.7.0/24",
				statusInfoChecker:  makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationFailure, messagePattern: "Role is undefined"},
					HasCPUCoresForRole:   {status: ValidationPending, messagePattern: "Missing inventory or role"},
					HasMemoryForRole:     {status: ValidationPending, messagePattern: "Missing inventory or role"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationFailure, messagePattern: "Host does not belong to machine network CIDR"},
				}),
				inventory:     workerInventory(),
				errorExpected: false,
			},
			{
				name:              "known to pending",
				checkedInAt:       strfmt.DateTime(time.Now()),
				srcState:          HostStatusKnown,
				dstState:          HostStatusPendingForInput,
				role:              "worker",
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
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
				checkedInAt:       strfmt.DateTime(time.Now()),
				srcState:          HostStatusPendingForInput,
				dstState:          HostStatusPendingForInput,
				role:              "worker",
				statusInfoChecker: makeValueChecker(statusInfoPendingForInput),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationFailure, messagePattern: "Machine network CIDR is undefined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
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
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusDisconnected,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "5.6.7.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
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
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusDiscovering,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "5.6.7.0/24",
				role:               "master",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
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
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusInsufficient,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "master",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
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
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusPendingForInput,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "master",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				inventory:          workerInventory(),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationFailure, messagePattern: "Require at least 4 CPU cores for master role, found only 2"},
					HasMemoryForRole:     {status: ValidationFailure, messagePattern: "Require at least 16 GiB RAM role master, found only 8"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: "Hostname  is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				errorExpected: false,
			},
			{
				name:               "known to insufficient (2)",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusKnown,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "5.6.7.0/24",
				role:               "master",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
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
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusInsufficient,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "5.6.7.0/24",
				role:               "master",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
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
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusInsufficient,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "master",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
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
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusDiscovering,
				dstState:           HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "master",
				statusInfoChecker:  makeValueChecker(""),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsHostnameValid:      {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
				}),
				inventory:     masterInventory(),
				errorExpected: false,
			},
			{
				name:               "insufficient to known",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusInsufficient,
				dstState:           HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(""),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
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
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusPendingForInput,
				dstState:           HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(""),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
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
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusKnown,
				dstState:           HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "master",
				statusInfoChecker:  makeValueChecker(""),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role master"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role master"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
					IsHostnameValid:      {status: ValidationSuccess, messagePattern: "Hostname .* is allowed"},
				}),
				inventory:     masterInventory(),
				errorExpected: false,
			},
			{
				name:               "known to known with unexpected role",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusKnown,
				dstState:           HostStatusKnown,
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
				srcState = t.srcState
				host = getTestHost(hostId, clusterId, srcState)
				host.Inventory = t.inventory
				host.Role = models.HostRole(t.role)
				host.CheckedInAt = t.checkedInAt
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				cluster = getTestCluster(clusterId, t.machineNetworkCidr)
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
				if srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), common.GetEventSeverityFromHostStatus(t.dstState),
						gomock.Any(), gomock.Any(), host.ClusterID.String())
				}
				err := hapi.RefreshStatus(ctx, &host, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Role).To(Equal(models.HostRole(t.role)))
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
				dstState:      HostStatusError,
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
					mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), common.GetEventSeverityFromHostStatus(t.dstState),
						gomock.Any(), gomock.Any(), host.ClusterID.String())
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
			role                   string
			machineNetworkCidr     string
			checkedInAt            strfmt.DateTime
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
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusInsufficient,
				dstState:           HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(""),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:      masterInventoryWithHostname("first"),
				otherState:     HostStatusInsufficient,
				otherInventory: masterInventoryWithHostname("second"),
				errorExpected:  false,
			},
			{
				name:               "insufficient to insufficient (same hostname) 1",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusInsufficient,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:      masterInventoryWithHostname("first"),
				otherState:     HostStatusInsufficient,
				otherInventory: masterInventoryWithHostname("first"),
				errorExpected:  false,
			},
			{
				name:               "insufficient to insufficient (same hostname) 2",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusInsufficient,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:              masterInventoryWithHostname("first"),
				otherState:             HostStatusInsufficient,
				otherInventory:         masterInventoryWithHostname("second"),
				otherRequestedHostname: "first",
				errorExpected:          false,
			},
			{
				name:               "insufficient to insufficient (same hostname) 3",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusInsufficient,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:         masterInventoryWithHostname("first"),
				requestedHostname: "second",
				otherState:        HostStatusInsufficient,
				otherInventory:    masterInventoryWithHostname("second"),
				errorExpected:     false,
			},
			{
				name:               "insufficient to insufficient (same hostname) 4",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusInsufficient,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:              masterInventoryWithHostname("first"),
				requestedHostname:      "third",
				otherState:             HostStatusInsufficient,
				otherInventory:         masterInventoryWithHostname("second"),
				otherRequestedHostname: "third",
				errorExpected:          false,
			},
			{
				name:               "insufficient to known 2",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusInsufficient,
				dstState:           HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(""),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:              masterInventoryWithHostname("first"),
				requestedHostname:      "third",
				otherState:             HostStatusInsufficient,
				otherInventory:         masterInventoryWithHostname("second"),
				otherRequestedHostname: "forth",
				errorExpected:          false,
			},

			{
				name:               "known to known",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusKnown,
				dstState:           HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(""),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:      masterInventoryWithHostname("first"),
				otherState:     HostStatusInsufficient,
				otherInventory: masterInventoryWithHostname("second"),
				errorExpected:  false,
			},
			{
				name:               "known to insufficient (same hostname) 1",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusKnown,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:      masterInventoryWithHostname("first"),
				otherState:     HostStatusInsufficient,
				otherInventory: masterInventoryWithHostname("first"),
				errorExpected:  false,
			},
			{
				name:               "known to insufficient (same hostname) 2",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusKnown,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:              masterInventoryWithHostname("first"),
				otherState:             HostStatusInsufficient,
				otherInventory:         masterInventoryWithHostname("second"),
				otherRequestedHostname: "first",
				errorExpected:          false,
			},
			{
				name:               "known to insufficient (same hostname) 3",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusKnown,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:         masterInventoryWithHostname("first"),
				requestedHostname: "second",
				otherState:        HostStatusInsufficient,
				otherInventory:    masterInventoryWithHostname("second"),
				errorExpected:     false,
			},
			{
				name:               "known to insufficient (same hostname) 4",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusKnown,
				dstState:           HostStatusInsufficient,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(statusInfoNotReadyForInstall),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationFailure, messagePattern: " is not unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:              masterInventoryWithHostname("first"),
				requestedHostname:      "third",
				otherState:             HostStatusInsufficient,
				otherInventory:         masterInventoryWithHostname("second"),
				otherRequestedHostname: "third",
				errorExpected:          false,
			},
			{
				name:               "known to known 2",
				checkedInAt:        strfmt.DateTime(time.Now()),
				srcState:           HostStatusKnown,
				dstState:           HostStatusKnown,
				machineNetworkCidr: "1.2.3.0/24",
				role:               "worker",
				statusInfoChecker:  makeValueChecker(""),
				validationsChecker: makeJsonChecker(map[validationID]validationCheckResult{
					IsConnected:          {status: ValidationSuccess, messagePattern: "Host is connected"},
					HasInventory:         {status: ValidationSuccess, messagePattern: "Valid inventory exists for the host"},
					HasMinCPUCores:       {status: ValidationSuccess, messagePattern: "Sufficient CPU cores"},
					HasMinMemory:         {status: ValidationSuccess, messagePattern: "Sufficient minimum RAM"},
					HasMinValidDisks:     {status: ValidationSuccess, messagePattern: "Sufficient disk capacity"},
					IsMachineCidrDefined: {status: ValidationSuccess, messagePattern: "Machine network CIDR is defined"},
					IsRoleDefined:        {status: ValidationSuccess, messagePattern: "Role is defined"},
					HasCPUCoresForRole:   {status: ValidationSuccess, messagePattern: "Sufficient CPU cores for role worker"},
					HasMemoryForRole:     {status: ValidationSuccess, messagePattern: "Sufficient RAM for role worker"},
					IsHostnameUnique:     {status: ValidationSuccess, messagePattern: " is unique in cluster"},
					BelongsToMachineCidr: {status: ValidationSuccess, messagePattern: "Host belongs to machine network CIDR"},
				}),
				inventory:              masterInventoryWithHostname("first"),
				requestedHostname:      "third",
				otherState:             HostStatusInsufficient,
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
				host.Role = models.HostRole(t.role)
				host.CheckedInAt = t.checkedInAt
				host.RequestedHostname = t.requestedHostname
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				otherHost := getTestHost(otherHostID, clusterId, t.otherState)
				otherHost.RequestedHostname = t.otherRequestedHostname
				otherHost.Inventory = t.otherInventory
				Expect(db.Create(&otherHost).Error).ShouldNot(HaveOccurred())
				cluster = getTestCluster(clusterId, t.machineNetworkCidr)
				Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
				if !t.errorExpected && srcState != t.dstState {
					mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), models.EventSeverityInfo,
						gomock.Any(), gomock.Any(), clusterId.String())
				}

				err := hapi.RefreshStatus(ctx, &host, db)
				if t.errorExpected {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
				var resultHost models.Host
				Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostId.String(), clusterId.String()).Error).ToNot(HaveOccurred())
				Expect(resultHost.Role).To(Equal(models.HostRole(t.role)))
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
				mockEvents.EXPECT().AddEvent(gomock.Any(), hostId.String(), models.EventSeverityError,
					"Host master-hostname: updated status from \"installed\" to \"error\" (Installation has been aborted due cluster errors)",
					gomock.Any(), clusterId.String())
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
