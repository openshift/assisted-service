package cluster

import (
	context "context"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/internal/events"
	"github.com/filanov/bm-inventory/internal/host"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("insufficient_state", func() {
	var (
		ctx          = context.Background()
		manager      API
		db           *gorm.DB
		currentState = models.ClusterStatusInsufficient
		id           strfmt.UUID
		cluster      common.Cluster
		ctrl         *gomock.Controller
		mockHostAPI  *host.MockAPI
		dbName       = "cluster_insufficient_state"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockHostAPI = host.NewMockAPI(ctrl)
		mockEvents := events.NewMockHandler(ctrl)
		db = common.PrepareTestDB(dbName)
		manager = &Manager{
			log:             getTestLog(),
			insufficient:    NewInsufficientState(getTestLog(), db, mockHostAPI),
			registrationAPI: NewRegistrar(getTestLog(), db),
			eventsHandler:   mockEvents,
		}

		id = strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(currentState),
		}}

		mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
		replyErr := manager.RegisterCluster(ctx, &cluster)
		Expect(replyErr).Should(BeNil())
		Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusInsufficient))
		c := geCluster(*cluster.ID, db)
		Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInsufficient))
	})

	mockHostAPIIsRequireUserActionResetFalse := func(times int) {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).Times(times)
	}

	mockHostAPIIsRequireUserActionResetTrue := func(times int) {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(true).Times(times)
	}

	mockHostAPIResetPendingUserActionSuccess := func(times int) {
		mockHostAPI.EXPECT().ResetPendingUserAction(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil).Times(times)
	}

	Context("refresh_state", func() {
		It("not answering requirement to be ready", func() {
			refreshedCluster, updateErr := manager.RefreshStatus(ctx, &cluster, db)
			Expect(updateErr).Should(BeNil())
			Expect(*refreshedCluster.Status).Should(Equal(models.ClusterStatusInsufficient))
		})

		It("resetting when host in reboot stage", func() {
			addHost(models.HostRoleMaster, models.HostStatusResetting, *cluster.ID, db)
			c := geCluster(*cluster.ID, db)
			Expect(len(c.Hosts)).Should(Equal(1))
			updateHostProgress(c.Hosts[0], models.HostStageRebooting, "rebooting", db)
			mockHostAPIIsRequireUserActionResetTrue(1)
			mockHostAPIResetPendingUserActionSuccess(1)
			refreshedCluster, updateErr := manager.RefreshStatus(ctx, &c, db)
			Expect(updateErr).Should(BeNil())
			Expect(*refreshedCluster.Status).Should(Equal(models.ClusterStatusInsufficient))
		})

		It("answering requirement to be ready", func() {
			addInstallationRequirements(id, db)
			mockHostAPIIsRequireUserActionResetFalse(3)
			refreshedCluster, updateErr := manager.RefreshStatus(ctx, &cluster, db)
			Expect(updateErr).Should(BeNil())
			Expect(*refreshedCluster.Status).Should(Equal(models.ClusterStatusReady))

		})
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})
})
