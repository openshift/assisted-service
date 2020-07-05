package host

import (
	"context"
	"time"

	"github.com/go-openapi/swag"

	"github.com/filanov/bm-inventory/internal/connectivity"
	"github.com/filanov/bm-inventory/internal/events"
	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("insufficient_state", func() {
	var (
		ctx                       = context.Background()
		state                     API
		db                        *gorm.DB
		currentState              = HostStatusInsufficient
		host                      models.Host
		id, clusterId             strfmt.UUID
		updateReply               *UpdateReply
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
		state = &Manager{eventsHandler: mockEvents, insufficient: NewInsufficientState(getTestLog(), db, mockHWValidator, mockConnectivityValidator)}

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, currentState)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		expectedReply = &expect{expectedState: currentState}
		addTestCluster(clusterId, "1.2.3.5", "1.2.3.6", "1.2.3.0/24", db)
	})

	Context("update hw info", func() {
		It("update", func() {
			mockEvents.EXPECT().AddEvent(gomock.Any(), string(id), gomock.Any(), gomock.Any(), string(clusterId))
			expectedStatusInfo := mockConnectivityAndHwValidators(&host, mockHWValidator, mockConnectivityValidator, false, true, true)
			updateReply, updateErr = state.UpdateHwInfo(ctx, &host, "some hw info")
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			expectedReply.expectedState = HostStatusKnown
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.Inventory).Should(Equal(defaultInventory()))
				Expect(h.HardwareInfo).Should(Equal("some hw info"))
				Expect(swag.StringValue(h.StatusInfo)).Should(Equal(expectedStatusInfo))
			}
		})
	})

	Context("update inventory", func() {
		It("sufficient_hw", func() {
			mockEvents.EXPECT().AddEvent(gomock.Any(), string(id), gomock.Any(), gomock.Any(), string(clusterId))
			expectedStatusInfo := mockConnectivityAndHwValidators(&host, mockHWValidator, mockConnectivityValidator, false, true, true)
			updateReply, updateErr = state.UpdateInventory(ctx, &host, "some hw info")
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			expectedReply.expectedState = HostStatusKnown
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.Inventory).Should(Equal("some hw info"))
				Expect(swag.StringValue(h.StatusInfo)).Should(Equal(expectedStatusInfo))
			}
		})
		It("insufficient_hw", func() {
			expectedStatusInfo := mockConnectivityAndHwValidators(&host, mockHWValidator, mockConnectivityValidator, false, false, true)
			updateReply, updateErr = state.UpdateInventory(ctx, &host, "some hw info")
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			expectedReply.expectedState = HostStatusInsufficient
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.Inventory).Should(Equal("some hw info"))
				Expect(swag.StringValue(h.StatusInfo)).Should(Equal(expectedStatusInfo))
			}
		})
		It("hw_validation_error", func() {
			expectedStatusInfo := mockConnectivityAndHwValidators(&host, mockHWValidator, mockConnectivityValidator, true, false, true)
			updateReply, updateErr = state.UpdateInventory(ctx, &host, "some hw info")
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			expectedReply.expectedState = HostStatusInsufficient
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.Inventory).Should(Equal(defaultInventory()))
				Expect(swag.StringValue(h.StatusInfo)).Should(Equal(expectedStatusInfo))
			}
		})
		It("sufficient_hw_insufficient_connectivity", func() {
			expectedStatusInfo := mockConnectivityAndHwValidators(&host, mockHWValidator, mockConnectivityValidator, false, true, false)
			updateReply, updateErr = state.UpdateInventory(ctx, &host, "some hw info")
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			expectedReply.expectedState = HostStatusInsufficient
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.Inventory).Should(Equal("some hw info"))
				Expect(swag.StringValue(h.StatusInfo)).Should(Equal(expectedStatusInfo))
			}
		})
		It("sufficient_hw_sufficient_connectivity_insufficient_role", func() {
			host.Role = ""
			expectedStatusInfo := mockConnectivityAndHwValidators(&host, mockHWValidator, mockConnectivityValidator, false, true, true)
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			expectedReply.expectedState = HostStatusInsufficient
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(swag.StringValue(h.StatusInfo)).Should(Equal(expectedStatusInfo))
			}
		})
	})

	Context("refresh_status", func() {
		It("keep_alive", func() {
			mockEvents.EXPECT().AddEvent(gomock.Any(), string(id), gomock.Any(), gomock.Any(), string(clusterId))
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-time.Minute))
			host.Inventory = ""
			mockConnectivityAndHwValidators(&host, mockHWValidator, mockConnectivityValidator, false, true, true)
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			expectedReply.expectedState = HostStatusKnown
		})
		It("keep_alive_timeout", func() {
			mockEvents.EXPECT().AddEvent(gomock.Any(), string(id), gomock.Any(), gomock.Any(), string(clusterId))
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-time.Hour))
			mockConnectivityAndHwValidators(&host, mockHWValidator, mockConnectivityValidator, false, true, true)
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			expectedReply.expectedState = HostStatusDisconnected
		})
	})

	It("enable_host", func() {
		updateReply, updateErr = state.EnableHost(ctx, &host)
	})

	It("disable_host", func() {
		updateReply, updateErr = state.DisableHost(ctx, &host)
		expectedReply.expectedState = HostStatusDisabled
	})

	AfterEach(func() {
		ctrl.Finish()
		postValidation(expectedReply, currentState, db, id, clusterId, updateReply, updateErr)

		// cleanup
		db.Close()
		expectedReply = nil
		updateReply = nil
		updateErr = nil
	})
})
