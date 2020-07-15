package host

import (
	"context"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/internal/events"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RefreshStatus", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		ctrl              *gomock.Controller
		mockEvents        *events.MockHandler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		db = prepareDB()
		hapi = NewManager(getTestLog(), db, mockEvents, nil, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	createClusterInState := func(state string) {
		cluster := common.Cluster{Cluster: models.Cluster{
			ID:     &clusterId,
			Status: swag.String(state),
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	}

	It("no change", func() {
		createClusterInState(models.ClusterStatusPreparingForInstallation)
		host = getTestHost(hostId, clusterId, models.HostStatusPreparingForInstallation)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		_, err := hapi.RefreshStatus(ctx, &host, db)
		Expect(err).ShouldNot(HaveOccurred())
		h := getHost(hostId, clusterId, db)
		Expect(swag.StringValue(h.Status)).To(Equal(models.HostStatusPreparingForInstallation))
	})

	It("cluster no longer preparing-for-installation - host should move to error", func() {
		createClusterInState(models.ClusterStatusError)
		host = getTestHost(hostId, clusterId, models.HostStatusPreparingForInstallation)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		mockEvents.EXPECT().AddEvent(gomock.Any(), string(hostId), gomock.Any(), gomock.Any(), string(clusterId)).
			Times(1)

		_, err := hapi.RefreshStatus(ctx, &host, db)
		Expect(err).ShouldNot(HaveOccurred())
		h := getHost(hostId, clusterId, db)
		Expect(swag.StringValue(h.Status)).To(Equal(models.HostStatusError))
	})

	AfterEach(func() {
		_ = db.Close()
		ctrl.Finish()
	})
})
