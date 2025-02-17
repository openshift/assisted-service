package host

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	commontesting "github.com/openshift/assisted-service/internal/common/testing"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/leader"
	"gorm.io/gorm"
)

var _ = Describe("monitor_disconnection", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		host          models.Host
		ctrl          *gomock.Controller
		mockEvents    *eventsapi.MockHandler
		dbName        string
		mockMetricApi *metrics.MockAPI
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		mockHwValidator := hardware.NewMockValidator(ctrl)
		mockMetricApi = metrics.NewMockAPI(ctrl)
		mockHwValidator.EXPECT().ListEligibleDisks(gomock.Any()).AnyTimes()
		mockHwValidator.EXPECT().GetClusterHostRequirements(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(&models.ClusterHostRequirements{
			Total: &models.ClusterHostRequirementsDetails{},
		}, nil)
		mockHwValidator.EXPECT().GetPreflightHardwareRequirements(gomock.Any(), gomock.Any()).AnyTimes().Return(&models.PreflightHardwareRequirements{
			Ocp: &models.HostTypeHardwareRequirementsWrapper{
				Worker: &models.HostTypeHardwareRequirements{
					Quantitative: &models.ClusterHostRequirementsDetails{},
				},
				Master: &models.HostTypeHardwareRequirements{
					Quantitative: &models.ClusterHostRequirementsDetails{},
				},
			},
		}, nil)
		mockHwValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(nil, nil).AnyTimes()
		mockOperators := operators.NewMockAPI(ctrl)
		pr := registry.NewMockProviderRegistry(ctrl)
		pr.EXPECT().IsHostSupported(commontesting.EqPlatformType(models.PlatformTypeBaremetal), gomock.Any()).Return(true, nil).AnyTimes()
		pr.EXPECT().IsHostSupported(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any()).Return(false, nil).AnyTimes()
		mockVersions := versions.NewMockHandler(ctrl)
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&models.ReleaseImage{URL: swag.String("quay.io/openshift/some-image::latest")}, nil).AnyTimes()
		state = NewManager(common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, mockHwValidator, nil, createValidatorCfg(),
			mockMetricApi, defaultConfig, dummy, mockOperators, pr, false, nil, mockVersions, false)
		clusterID := strfmt.UUID(uuid.New().String())
		infraEnvID := strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), infraEnvID, clusterID, models.HostStatusDiscovering)
		cluster := hostutil.GenerateTestCluster(clusterID)
		Expect(db.Save(&cluster).Error).ToNot(HaveOccurred())
		host.Inventory = workerInventory()
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostRegistrationSucceededEventName),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithClusterIdMatcher(clusterID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
		err := state.RegisterHost(ctx, &host, db)
		Expect(err).ShouldNot(HaveOccurred())
		db.First(&host, "id = ? and cluster_id = ?", host.ID, host.ClusterID)

		mockMetricApi.EXPECT().Duration("HostMonitoring", gomock.Any()).Times(1)
		mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(1)
		mockOperators.EXPECT().ValidateHost(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDMtvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDOscRequirementsSatisfied)},
		}, nil)
		mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("abc").AnyTimes()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("host_disconnecting", func() {
		It("known_host_disconnects", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
			host.Status = swag.String(models.HostStatusKnown)
			db.Save(&host)
		})

		It("discovering_host_disconnects", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
			host.Status = swag.String(models.HostStatusDiscovering)
			db.Save(&host)
		})

		It("known_host_insufficient", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
			host.Status = swag.String(models.HostStatusInsufficient)
			db.Save(&host)
		})

		AfterEach(func() {
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(host.ID.String()),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String())))
			state.HostMonitoring()
			db.First(&host, "id = ? and cluster_id = ?", host.ID, host.ClusterID)
			Expect(*host.Status).Should(Equal(models.HostStatusDisconnected))
		})
	})

	Context("host_reconnecting", func() {
		It("host_connects", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now())
			host.Inventory = ""
			host.Status = swag.String(models.HostStatusDisconnected)
			db.Save(&host)
		})

		AfterEach(func() {
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
				eventstest.WithHostIdMatcher(host.ID.String()),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String())))
			state.HostMonitoring()
			db.First(&host, "id = ? and cluster_id = ?", host.ID, host.ClusterID)
			Expect(*host.Status).Should(Equal(models.HostStatusDiscovering))
		})
	})

	AfterEach(func() {
		ctrl.Finish()
		common.CloseDB(db)
	})
})

var _ = Describe("TestHostMonitoring - with cluster", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		host          models.Host
		ctrl          *gomock.Controller
		cfg           Config
		mockEvents    *eventsapi.MockHandler
		dbName        string
		clusterID     = strfmt.UUID(uuid.New().String())
		infraEnvID    = strfmt.UUID(uuid.New().String())
		mockMetricApi *metrics.MockAPI
		mockVersions  *versions.MockHandler
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), gomock.Any()).AnyTimes()

		Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).ShouldNot(HaveOccurred())
		mockMetricApi = metrics.NewMockAPI(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		mockHwValidator.EXPECT().ListEligibleDisks(gomock.Any()).AnyTimes()
		mockHwValidator.EXPECT().GetClusterHostRequirements(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(&models.ClusterHostRequirements{
			Total: &models.ClusterHostRequirementsDetails{},
		}, nil)
		mockHwValidator.EXPECT().GetPreflightHardwareRequirements(gomock.Any(), gomock.Any()).AnyTimes().Return(&models.PreflightHardwareRequirements{
			Ocp: &models.HostTypeHardwareRequirementsWrapper{
				Worker: &models.HostTypeHardwareRequirements{
					Quantitative: &models.ClusterHostRequirementsDetails{},
				},
				Master: &models.HostTypeHardwareRequirements{
					Quantitative: &models.ClusterHostRequirementsDetails{},
				},
			},
		}, nil)
		mockHwValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(nil, nil).AnyTimes()
		mockOperators := operators.NewMockAPI(ctrl)
		pr := registry.NewMockProviderRegistry(ctrl)
		pr.EXPECT().IsHostSupported(commontesting.EqPlatformType(models.PlatformTypeBaremetal), gomock.Any()).Return(true, nil).AnyTimes()
		pr.EXPECT().IsHostSupported(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any()).Return(false, nil).AnyTimes()
		mockVersions = versions.NewMockHandler(ctrl)
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&models.ReleaseImage{URL: swag.String("quay.io/openshift/some-image::latest")}, nil).AnyTimes()
		state = NewManager(common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, mockHwValidator, nil, createValidatorCfg(),
			mockMetricApi, &cfg, &leader.DummyElector{}, mockOperators, pr, false, nil, mockVersions, false)

		mockMetricApi.EXPECT().Duration("HostMonitoring", gomock.Any()).Times(1)
		mockOperators.EXPECT().ValidateHost(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDMtvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDOscRequirementsSatisfied)},
		}, nil)
		mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("abc").AnyTimes()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	Context("auto-assign reset", func() {
		addHost := func(clusterID strfmt.UUID, role models.HostRole, status, kind string) {
			host = hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), infraEnvID, clusterID, status)
			host.Inventory = ""
			host.Role = models.HostRoleAutoAssign
			host.SuggestedRole = role
			Expect(state.RegisterHost(ctx, &host, db)).ShouldNot(HaveOccurred())
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
			host.Status = swag.String(status)
			host.Kind = swag.String(kind)
			db.Save(&host)
		}
		registerClusterWithAutoAssignHostInStatus := func(status, kind string) {
			clusterID = strfmt.UUID(uuid.New().String())
			cluster := hostutil.GenerateTestCluster(clusterID)
			Expect(db.Save(&cluster).Error).ToNot(HaveOccurred())
			addHost(clusterID, models.HostRoleAutoAssign, status, kind)
			addHost(clusterID, models.HostRoleWorker, models.HostStatusKnown, kind)
		}

		It("do not reset with status disconnected", func() {
			var count int64
			registerClusterWithAutoAssignHostInStatus(models.HostStatusDisconnected, models.HostKindHost)
			mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(2)
			state.HostMonitoring()
			Expect(db.Model(&models.Host{}).Where("suggested_role = ?", models.HostRoleAutoAssign).Count(&count).Error).
				ShouldNot(HaveOccurred())
			Expect(count).Should(Equal(int64(1)))
		})

		It("do not reset when host is orphan", func() {
			var count int64
			//define 2 hosts with cluster id in the clusters table
			registerClusterWithAutoAssignHostInStatus(models.HostStatusKnown, models.HostKindHost)
			//define 2 orphan hosts
			otherClusterId := strfmt.UUID(uuid.New().String())
			addHost(otherClusterId, models.HostRoleAutoAssign, models.HostStatusKnown, models.HostKindHost)
			addHost(otherClusterId, models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost)
			mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(2)
			state.HostMonitoring()
			Expect(db.Model(&models.Host{}).Where("suggested_role = ?", models.HostRoleAutoAssign).Count(&count).Error).
				ShouldNot(HaveOccurred())
			//expect 3 hosts with suggested = auto-assign: 2 original ones and one resetted host
			//where the cluster is in the clusters table
			Expect(count).Should(Equal(int64(3)))
		})

		It("do not reset with day2 host", func() {
			var count int64
			registerClusterWithAutoAssignHostInStatus(models.HostStatusKnown, models.HostKindAddToExistingClusterHost)
			mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(2)
			state.HostMonitoring()
			Expect(db.Model(&models.Host{}).Where("suggested_role = ?", models.HostRoleAutoAssign).Count(&count).Error).
				ShouldNot(HaveOccurred())
			Expect(count).Should(Equal(int64(1)))
		})
	})

	Context("validate monitor in batches", func() {
		registerAndValidateDisconnected := func(nHosts int) {
			for i := 0; i < nHosts; i++ {
				if i%10 == 0 {
					clusterID = strfmt.UUID(uuid.New().String())
					cluster := hostutil.GenerateTestCluster(clusterID)
					Expect(db.Save(&cluster).Error).ToNot(HaveOccurred())
				}
				host = hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), infraEnvID, clusterID, models.HostStatusDiscovering)
				host.Inventory = workerInventory()
				Expect(state.RegisterHost(ctx, &host, db)).ShouldNot(HaveOccurred())
				host.CheckedInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
				db.Save(&host)
			}
			state.HostMonitoring()
			var count int64
			Expect(db.Model(&models.Host{}).Where("status = ?", models.HostStatusDisconnected).Count(&count).Error).
				ShouldNot(HaveOccurred())
			Expect(count).Should(Equal(int64(nHosts)))
		}

		It("5 hosts all disconnected", func() {
			mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(5)
			registerAndValidateDisconnected(5)
		})

		It("15 hosts all disconnected", func() {
			mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(15)
			registerAndValidateDisconnected(15)
		})

		It("765 hosts all disconnected", func() {
			mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(765)
			registerAndValidateDisconnected(765)
		})
	})

	Context("validate host status monitoring", func() {
		DescribeTable("status", func(clusterStatus string, hostStatus string, logState models.LogsState, expectedCount int) {
			clusterID = strfmt.UUID(uuid.New().String())
			cluster := hostutil.GenerateTestCluster(clusterID)
			cluster.Status = swag.String(clusterStatus)
			Expect(db.Save(&cluster).Error).ToNot(HaveOccurred())

			host = hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), infraEnvID, clusterID, models.HostStatusDiscovering)
			host.Inventory = workerInventory()
			host.LogsInfo = logState
			Expect(state.RegisterHost(ctx, &host, db)).ShouldNot(HaveOccurred())
			Expect(db.Model(&host).Update("status", hostStatus).Error).ShouldNot(HaveOccurred())

			mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(expectedCount)
			state.HostMonitoring()
		},
			Entry("HostStatusAddedToExistingCluster is not monitored", models.ClusterStatusReady, models.HostStatusAddedToExistingCluster, models.LogsStateCompleted, 0),
			Entry("HostStatusBinding is not monitored", models.ClusterStatusReady, models.HostStatusBinding, models.LogsStateCompleted, 0),
			Entry("HostStatusCancelled is monitored when waiting for log state", models.ClusterStatusReady, models.HostStatusCancelled, models.LogsStateCollecting, 1),
			Entry("HostStatusCancelled is not monitored when log state has timed out", models.ClusterStatusReady, models.HostStatusCancelled, models.LogsStateTimeout, 0),
			Entry("HostStatusCancelled is not monitored when log state is completed", models.ClusterStatusReady, models.HostStatusCancelled, models.LogsStateCompleted, 0),
			Entry("HostStatusCancelled is not monitored when log state is empty", models.ClusterStatusReady, models.HostStatusCancelled, models.LogsStateEmpty, 0),
			Entry("HostStatusDisabled is not monitored", models.ClusterStatusReady, models.HostStatusDisabled, models.LogsStateCompleted, 0),
			Entry("HostStatusDisabledUnbound is not monitored", models.ClusterStatusReady, models.HostStatusDisabledUnbound, models.LogsStateCompleted, 0),
			Entry("HostStatusDisconnected is monitored", models.ClusterStatusReady, models.HostStatusDisconnected, models.LogsStateCompleted, 1),
			Entry("HostStatusDisconnectedUnbound is not monitored", models.ClusterStatusReady, models.HostStatusDisconnectedUnbound, models.LogsStateCompleted, 0),
			Entry("HostStatusDiscovering is monitored", models.ClusterStatusReady, models.HostStatusDiscovering, models.LogsStateCompleted, 1),
			Entry("HostStatusDiscoveringUnbound is not monitored", models.ClusterStatusReady, models.HostStatusDiscoveringUnbound, models.LogsStateCompleted, 0),
			Entry("HostStatusError is monitored when waiting for log state", models.ClusterStatusReady, models.HostStatusError, models.LogsStateCollecting, 1),
			Entry("HostStatusError is not monitored when log state has timed out", models.ClusterStatusReady, models.HostStatusError, models.LogsStateTimeout, 0),
			Entry("HostStatusError is not monitored when log state is completed", models.ClusterStatusReady, models.HostStatusError, models.LogsStateCompleted, 0),
			Entry("HostStatusError is not monitored when log state is empty", models.ClusterStatusReady, models.HostStatusError, models.LogsStateEmpty, 0),
			Entry("HostStatusInstalled is monitored when cluster status not installed", models.ClusterStatusReady, models.HostStatusInstalled, models.LogsStateCompleted, 1),
			Entry("HostStatusInstalled is not monitored when cluster status is installed", models.ClusterStatusInstalled, models.HostStatusInstalled, models.LogsStateCompleted, 0),
			Entry("HostStatusInstalling is monitored", models.ClusterStatusReady, models.HostStatusInstalling, models.LogsStateCompleted, 1),
			Entry("HostStatusInstallingInProgress is monitored", models.ClusterStatusReady, models.HostStatusInstallingInProgress, models.LogsStateCompleted, 1),
			Entry("HostStatusInstallingPendingUserAction is monitored", models.ClusterStatusReady, models.HostStatusInstallingPendingUserAction, models.LogsStateCompleted, 1),
			Entry("HostStatusInsufficient is monitored", models.ClusterStatusReady, models.HostStatusInsufficient, models.LogsStateCompleted, 1),
			Entry("HostStatusInsufficientUnbound is not monitored", models.ClusterStatusReady, models.HostStatusInsufficientUnbound, models.LogsStateCompleted, 0),
			Entry("HostStatusKnown is monitored", models.ClusterStatusReady, models.HostStatusKnown, models.LogsStateCompleted, 1),
			Entry("HostStatusKnownUnbound is not monitored", models.ClusterStatusReady, models.HostStatusKnownUnbound, models.LogsStateCompleted, 0),
			Entry("HostStatusPendingForInput is monitored", models.ClusterStatusReady, models.HostStatusPendingForInput, models.LogsStateCompleted, 1),
			Entry("HostStatusPreparingFailed is monitored", models.ClusterStatusReady, models.HostStatusPreparingFailed, models.LogsStateCompleted, 1),
			Entry("HostStatusPreparingForInstallation is monitored", models.ClusterStatusReady, models.HostStatusPreparingForInstallation, models.LogsStateCompleted, 1),
			Entry("HostStatusPreparingSuccessful is monitored", models.ClusterStatusReady, models.HostStatusPreparingSuccessful, models.LogsStateCompleted, 1),
			Entry("HostStatusReclaiming is not monitored", models.ClusterStatusReady, models.HostStatusReclaiming, models.LogsStateCompleted, 0),
			Entry("HostStatusReclaimingRebooting is not monitored", models.ClusterStatusReady, models.HostStatusReclaimingRebooting, models.LogsStateCompleted, 0),
			Entry("HostStatusResetting is not monitored", models.ClusterStatusReady, models.HostStatusResetting, models.LogsStateCompleted, 0),
			Entry("HostStatusResettingPendingUserAction is monitored", models.ClusterStatusReady, models.HostStatusResettingPendingUserAction, models.LogsStateCompleted, 1),
			Entry("HostStatusUnbinding is not monitored", models.ClusterStatusReady, models.HostStatusUnbinding, models.LogsStateCompleted, 0),
			Entry("HostStatusUnbindingPendingUserAction is not monitored", models.ClusterStatusReady, models.HostStatusUnbindingPendingUserAction, models.LogsStateCompleted, 0),
		)
	})
})

var _ = Describe("HostMonitoring - with infra-env", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		host          models.Host
		ctrl          *gomock.Controller
		cfg           Config
		mockEvents    *eventsapi.MockHandler
		dbName        string
		infraEnvID    = strfmt.UUID(uuid.New().String())
		mockMetricApi *metrics.MockAPI
		mockVersions  *versions.MockHandler
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), gomock.Any()).AnyTimes()
		Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).ShouldNot(HaveOccurred())
		mockMetricApi = metrics.NewMockAPI(ctrl)
		mockHwValidator := hardware.NewMockValidator(ctrl)
		mockHwValidator.EXPECT().ListEligibleDisks(gomock.Any()).AnyTimes()
		mockHwValidator.EXPECT().GetInfraEnvHostRequirements(gomock.Any(), gomock.Any()).AnyTimes().Return(&models.ClusterHostRequirements{
			Total: &models.ClusterHostRequirementsDetails{},
		}, nil)
		mockHwValidator.EXPECT().GetPreflightInfraEnvHardwareRequirements(gomock.Any(), gomock.Any()).AnyTimes().Return(&models.PreflightHardwareRequirements{
			Ocp: &models.HostTypeHardwareRequirementsWrapper{
				Worker: &models.HostTypeHardwareRequirements{
					Quantitative: &models.ClusterHostRequirementsDetails{},
				},
				Master: &models.HostTypeHardwareRequirements{
					Quantitative: &models.ClusterHostRequirementsDetails{},
				},
			},
		}, nil)
		mockHwValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(nil, nil).AnyTimes()
		mockOperators := operators.NewMockAPI(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&models.ReleaseImage{URL: swag.String("quay.io/openshift/some-image::latest")}, nil).AnyTimes()
		state = NewManager(common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, mockHwValidator, nil, createValidatorCfg(),
			mockMetricApi, &cfg, &leader.DummyElector{}, mockOperators, nil, false, nil, mockVersions, false)

		mockMetricApi.EXPECT().Duration("HostMonitoring", gomock.Any()).Times(1)
		mockOperators.EXPECT().ValidateHost(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDMtvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDOscRequirementsSatisfied)},
		}, nil)
		mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("abc").AnyTimes()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	Context("validate monitor in batches", func() {
		registerAndValidateDisconnected := func(nHosts int) {
			for i := 0; i < nHosts; i++ {
				if i%10 == 0 {
					infraEnvID = strfmt.UUID(uuid.New().String())
					infraEnv := hostutil.GenerateTestInfraEnv(infraEnvID)
					Expect(db.Save(infraEnv).Error).ToNot(HaveOccurred())
				}
				host = hostutil.GenerateTestHostWithInfraEnv(strfmt.UUID(uuid.New().String()), infraEnvID, models.HostStatusDiscoveringUnbound, models.HostRoleWorker)
				host.Inventory = workerInventory()
				Expect(state.RegisterHost(ctx, &host, db)).ShouldNot(HaveOccurred())
				host.CheckedInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
				db.Save(&host)
			}
			state.HostMonitoring()
			var count int64
			Expect(db.Model(&models.Host{}).Where("status = ?", models.HostStatusDisconnectedUnbound).Count(&count).Error).
				ShouldNot(HaveOccurred())
			Expect(count).Should(Equal(int64(nHosts)))
		}

		It("5 hosts all disconnected", func() {
			mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(5)
			registerAndValidateDisconnected(5)
		})

		It("15 hosts all disconnected", func() {
			mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(15)
			registerAndValidateDisconnected(15)
		})

		It("765 hosts all disconnected", func() {
			mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(765)
			registerAndValidateDisconnected(765)
		})
	})

	Context("with an infraEnv", func() {
		var infraEnvID strfmt.UUID
		BeforeEach(func() {
			infraEnvID = strfmt.UUID(uuid.New().String())
			infraEnv := hostutil.GenerateTestInfraEnv(infraEnvID)
			Expect(db.Save(infraEnv).Error).ToNot(HaveOccurred())
		})

		createTimeoutHostWithStatus := func(status string) strfmt.UUID {
			host = hostutil.GenerateTestHostWithInfraEnv(strfmt.UUID(uuid.New().String()), infraEnvID, status, models.HostRoleWorker)
			host.StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-70 * time.Minute))
			host.Inventory = workerInventory()
			Expect(db.Create(&host).Error).ToNot(HaveOccurred())
			return *host.ID
		}

		It("times out Reclaiming hosts", func() {
			mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(1)
			hostID := createTimeoutHostWithStatus(models.HostStatusReclaiming)
			state.HostMonitoring()
			h := hostutil.GetHostFromDB(hostID, infraEnvID, db)
			Expect(*h.Status).To(Equal(models.HostStatusUnbindingPendingUserAction))
		})

		It("times out ReclaimingRebooting hosts", func() {
			mockMetricApi.EXPECT().MonitoredHostsDurationMs(gomock.Any()).Times(1)
			hostID := createTimeoutHostWithStatus(models.HostStatusReclaimingRebooting)
			state.HostMonitoring()
			h := hostutil.GetHostFromDB(hostID, infraEnvID, db)
			Expect(*h.Status).To(Equal(models.HostStatusUnbindingPendingUserAction))
		})
	})
})
