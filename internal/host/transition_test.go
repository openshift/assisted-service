package host

import (
	"context"

	"github.com/go-openapi/swag"

	. "github.com/onsi/gomega"

	"github.com/filanov/bm-inventory/models"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
)

var _ = Describe("RegisterHost", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
	)

	BeforeEach(func() {
		db = prepareDB()
		hapi = NewManager(getTestLog(), db, nil, nil, nil, nil)
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
			Expect(h.HardwareInfo).Should(Equal(defaultHwInfo))
			Expect(h.StatusInfo).NotTo(BeNil())
		})

		for i := range tests {
			t := tests[i]

			It(t.name, func() {
				Expect(db.Create(&models.Host{
					ID:           &hostId,
					ClusterID:    clusterId,
					Role:         models.HostRoleMaster,
					HardwareInfo: defaultHwInfo,
					Status:       swag.String(t.srcState),
				}).Error).ShouldNot(HaveOccurred())

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
			Expect(h.HardwareInfo).Should(Equal(""))
			Expect(h.DiscoveryAgentVersion).To(Equal(discoveryAgentVersion))
		})

		for i := range tests {
			t := tests[i]

			It(t.name, func() {
				Expect(db.Create(&models.Host{
					ID:           &hostId,
					ClusterID:    clusterId,
					Role:         models.HostRoleMaster,
					HardwareInfo: defaultHwInfo,
					Status:       swag.String(t.srcState),
				}).Error).ShouldNot(HaveOccurred())

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
					ID:           &hostId,
					ClusterID:    clusterId,
					Role:         models.HostRoleMaster,
					HardwareInfo: defaultHwInfo,
					Status:       swag.String(t.srcState),
				}).Error).ShouldNot(HaveOccurred())

				Expect(hapi.RegisterHost(ctx, &models.Host{
					ID:        &hostId,
					ClusterID: clusterId,
					Status:    swag.String(t.srcState),
				})).Should(HaveOccurred())

				h := getHost(hostId, clusterId, db)
				Expect(swag.StringValue(h.Status)).Should(Equal(t.srcState))
				Expect(h.Role).Should(Equal(models.HostRoleMaster))
				Expect(h.HardwareInfo).Should(Equal(defaultHwInfo))
			})
		}
	})

	Context("register after reboot", func() {
		tests := []struct {
			name     string
			srcState string
			progress models.HostProgress
		}{
			{
				name:     "host in reboot",
				srcState: HostStatusInstallingInProgress,
				progress: models.HostProgress{
					CurrentStage: models.HostStageRebooting,
				},
			},
		}

		AfterEach(func() {
			h := getHost(hostId, clusterId, db)
			Expect(swag.StringValue(h.Status)).Should(Equal(models.HostStatusInstallingPendingUserAction))
			Expect(h.Role).Should(Equal(models.HostRoleMaster))
			Expect(h.HardwareInfo).Should(Equal(defaultHwInfo))
			Expect(h.StatusInfo).NotTo(BeNil())
		})

		for i := range tests {
			t := tests[i]

			It(t.name, func() {
				Expect(db.Create(&models.Host{
					ID:           &hostId,
					ClusterID:    clusterId,
					Role:         models.HostRoleMaster,
					HardwareInfo: defaultHwInfo,
					Status:       swag.String(t.srcState),
					Progress:     &t.progress,
				}).Error).ShouldNot(HaveOccurred())

				Expect(hapi.RegisterHost(ctx, &models.Host{
					ID:        &hostId,
					ClusterID: clusterId,
					Status:    swag.String(t.srcState),
				})).ShouldNot(HaveOccurred())
			})
		}
	})

	AfterEach(func() {
		_ = db.Close()
	})
})

var _ = Describe("HostInstallationFailed", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
	)

	BeforeEach(func() {
		db = prepareDB()
		hapi = NewManager(getTestLog(), db, nil, nil, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(hostId, clusterId, "")
		host.Status = swag.String(HostStatusInstalling)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("handle_installation_error", func() {
		Expect(hapi.HandleInstallationFailure(ctx, &host)).ShouldNot(HaveOccurred())
		h := getHost(hostId, clusterId, db)
		Expect(swag.StringValue(h.Status)).Should(Equal(HostStatusError))
		Expect(swag.StringValue(h.StatusInfo)).Should(Equal("installation command failed"))
	})

	AfterEach(func() {
		_ = db.Close()
	})
})

var _ = Describe("Install", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
	)

	BeforeEach(func() {
		db = prepareDB()
		hapi = NewManager(getTestLog(), db, nil, nil, nil, nil)
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
			Expect(hapi.Install(ctx, &host, tx)).ShouldNot(HaveOccurred())
			Expect(tx.Commit().Error).ShouldNot(HaveOccurred())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(HostStatusInstalling))
			Expect(*h.StatusInfo).Should(Equal(statusInfoInstalling))
		})

		It("rollback transition", func() {
			tx := db.Begin()
			Expect(tx.Error).To(BeNil())
			Expect(hapi.Install(ctx, &host, tx)).ShouldNot(HaveOccurred())
			Expect(tx.Rollback().Error).ShouldNot(HaveOccurred())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(models.HostStatusPreparingForInstallation))
			Expect(*h.StatusInfo).Should(Equal(models.HostStatusPreparingForInstallation))
		})
	})

	AfterEach(func() {
		_ = db.Close()
	})
})

var _ = Describe("Disable", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
	)

	BeforeEach(func() {
		db = prepareDB()
		hapi = NewManager(getTestLog(), db, nil, nil, nil, nil)
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
				t.validation(hapi.DisableHost(ctx, &host))
			})
		}
	})

	AfterEach(func() {
		_ = db.Close()
	})
})

var _ = Describe("Enable", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
	)

	BeforeEach(func() {
		db = prepareDB()
		hapi = NewManager(getTestLog(), db, nil, nil, nil, nil)
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
			Expect(h.HardwareInfo).Should(Equal(""))
		}

		failure := func(reply error) {
			Expect(reply).Should(HaveOccurred())
			h := getHost(hostId, clusterId, db)
			Expect(*h.Status).Should(Equal(srcState))
			Expect(h.HardwareInfo).Should(Equal(defaultHwInfo))
		}

		tests := []struct {
			name       string
			srcState   string
			validation func(error)
		}{
			{
				name:       "known",
				srcState:   HostStatusKnown,
				validation: failure,
			},
			{
				name:       "disabled to enable",
				srcState:   HostStatusDisabled,
				validation: success,
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
				srcState = t.srcState
				host = getTestHost(hostId, clusterId, srcState)
				host.HardwareInfo = defaultHwInfo
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				t.validation(hapi.EnableHost(ctx, &host))
			})
		}
	})

	AfterEach(func() {
		_ = db.Close()
	})
})
