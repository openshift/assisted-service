package host

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/filanov/bm-inventory/internal/connectivity"
	"github.com/filanov/bm-inventory/internal/events"
	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
)

var _ = Describe("known_state", func() {
	var (
		ctx                       = context.Background()
		state                     API
		db                        *gorm.DB
		currentState              = HostStatusKnown
		host                      models.Host
		id, clusterId             strfmt.UUID
		hostAfterRefresh          *models.Host
		updateErr                 error
		expectedReply             *expect
		ctrl                      *gomock.Controller
		mockHWValidator           *hardware.MockValidator
		mockConnectivityValidator *connectivity.MockValidator
		mockEvents                *events.MockHandler
	)

	BeforeEach(func() {
		db = prepareDB()
		ctrl = gomock.NewController(GinkgoT())
		mockHWValidator = hardware.NewMockValidator(ctrl)
		mockConnectivityValidator = connectivity.NewMockValidator(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		state = &Manager{eventsHandler: mockEvents, known: NewKnownState(getTestLog(), db, mockHWValidator, mockConnectivityValidator)}

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, currentState)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		expectedReply = &expect{expectedStatus: currentState}
		addTestCluster(clusterId, "1.2.3.5", "1.2.3.6", "1.2.3.0/24", db)
	})

	Context("refresh_status", func() {
		It("keep_alive", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-time.Minute))
			host.Inventory = ""
			mockConnectivityAndHwValidators(&host, mockHWValidator, mockConnectivityValidator, false, true, true)
			hostAfterRefresh, updateErr = state.RefreshStatus(ctx, &host, db)
			expectedReply.expectedStatus = HostStatusKnown
		})
		It("keep_alive_timeout", func() {
			mockEvents.EXPECT().AddEvent(gomock.Any(), string(id), models.EventSeverityInfo, gomock.Any(), gomock.Any(), string(clusterId))
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-time.Hour))
			mockConnectivityAndHwValidators(&host, mockHWValidator, mockConnectivityValidator, false, true, true)
			hostAfterRefresh, updateErr = state.RefreshStatus(ctx, &host, db)
			expectedReply.expectedStatus = HostStatusDisconnected
		})
	})

	AfterEach(func() {
		ctrl.Finish()
		postValidation(expectedReply, currentState, db, id, clusterId, hostAfterRefresh, updateErr)
		// cleanup
		db.Close()
		expectedReply = nil
		hostAfterRefresh = nil
		updateErr = nil
	})
})
