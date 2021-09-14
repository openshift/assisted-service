package host

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/thoas/go-funk"
)

var _ = Describe("Suggested-Role on Refresh", func() {
	var (
		ctx                   = context.Background()
		hapi                  API
		db                    *gorm.DB
		clusterId, infraEnvId strfmt.UUID
		host                  models.Host
		cluster               common.Cluster
		mockEvents            *events.MockHandler
		ctrl                  *gomock.Controller
		dbName                string
		mockHwValidator       *hardware.MockValidator
		validatorCfg          *hardware.ValidatorCfg
		operatorsManager      *operators.Manager
	)

	initHwValidator := func() {
		mockHwValidator = hardware.NewMockValidator(ctrl)
		validatorCfg = createValidatorCfg()
		mockHwValidator.EXPECT().ListEligibleDisks(gomock.Any()).DoAndReturn(func(inventory *models.Inventory) []*models.Disk {
			// Mock the hwValidator behavior of performing simple filtering according to disk size, because these tests
			// rely on small disks to get filtered out.
			return funk.Filter(inventory.Disks, func(disk *models.Disk) bool {
				var minDiskSizeGb int64 = 120
				return disk.SizeBytes >= conversions.GibToBytes(minDiskSizeGb)
			}).([]*models.Disk)
		}).AnyTimes()
		mockHwValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(nil, nil).AnyTimes()
		mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("/dev/sda").AnyTimes()
	}

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())

		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		mockEvents = events.NewMockHandler(ctrl)
		operatorsManager = operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		initHwValidator()
		pr := registry.NewMockProviderRegistry(ctrl)
		pr.EXPECT().IsHostSupported(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		hapi = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, validatorCfg, nil, defaultConfig, nil, operatorsManager, pr)
	})

	tests := []struct {
		name           string
		inventory      string
		srcState       string
		suggested_role models.HostRole
		eventType      *string
	}{
		{
			name:           "insufficient worker memory --> suggested as worker",
			inventory:      hostutil.GenerateInventoryWithResourcesWithBytes(4, conversions.MibToBytes(150), conversions.MibToBytes(150), "worker"),
			srcState:       models.HostStatusDiscovering,
			suggested_role: models.HostRoleWorker,
			eventType:      &eventgen.HostStatusUpdatedEventName,
		},
		{
			name:           "sufficient master memory --> suggested as master when masters < 3",
			inventory:      hostutil.GenerateMasterInventory(),
			srcState:       models.HostStatusInsufficient,
			suggested_role: models.HostRoleMaster,
		},
		{
			name:           "sufficient worker memory --> suggested as worker",
			inventory:      workerInventory(),
			srcState:       models.HostStatusKnown,
			suggested_role: models.HostRoleWorker,
			eventType:      &eventgen.HostStatusUpdatedEventName,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			cluster = hostutil.GenerateTestCluster(clusterId, common.TestIPv4Networking.MachineNetworks)
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

			hostID := strfmt.UUID(uuid.New().String())
			host = hostutil.GenerateTestHost(hostID, infraEnvId, clusterId, t.srcState)
			host.Inventory = t.inventory
			host.Role = models.HostRoleAutoAssign
			host.SuggestedRole = models.HostRoleAutoAssign
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			mockDefaultClusterHostRequirements(mockHwValidator)
			if t.eventType != nil {
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(*t.eventType),
					eventstest.WithHostIdMatcher(host.ID.String()),
					eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				))
			}

			err := hapi.RefreshStatus(ctx, &host, db)
			Expect(err).ToNot(HaveOccurred())

			var resultHost models.Host
			Expect(db.Take(&resultHost, "id = ? and cluster_id = ?", hostID, clusterId.String()).Error).ToNot(HaveOccurred())
			Expect(resultHost.SuggestedRole).To(Equal(t.suggested_role))
		})
	}
})
