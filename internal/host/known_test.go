package host

import (
	"context"
	"time"

	"github.com/filanov/bm-inventory/internal/connectivity"
	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("known_state", func() {
	var (
		ctx                       = context.Background()
		state                     API
		db                        *gorm.DB
		currentState              = HostStatusKnown
		host                      models.Host
		id, clusterId             strfmt.UUID
		updateReply               *UpdateReply
		updateErr                 error
		expectedReply             *expect
		ctrl                      *gomock.Controller
		mockHWValidator           *hardware.MockValidator
		mockConnectivityValidator *connectivity.MockValidator
	)

	BeforeEach(func() {
		db = prepareDB()
		ctrl = gomock.NewController(GinkgoT())
		mockHWValidator = hardware.NewMockValidator(ctrl)
		mockConnectivityValidator = connectivity.NewMockValidator(ctrl)
		state = &Manager{known: NewKnownState(getTestLog(), db, mockHWValidator, mockConnectivityValidator)}

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, currentState)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		expectedReply = &expect{expectedState: currentState}
		addTestCluster(clusterId, "1.2.3.5", "1.2.3.6", "1.2.3.0/24", db)
	})

	Context("update hw info", func() {
		It("update", func() {
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

	Context("update_role", func() {
		It("master", func() {
			updateReply, updateErr = state.UpdateRole(ctx, &host, "master", nil)
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.Role).Should(Equal("master"))
			}
		})
		It("master_with_tx", func() {
			tx := db.Begin()
			Expect(tx.Error).ShouldNot(HaveOccurred())
			updateReply, updateErr = state.UpdateRole(ctx, &host, "master", tx)
			Expect(tx.Rollback().Error).ShouldNot(HaveOccurred())
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.Role).Should(Equal("worker"))
			}
		})
	})

	Context("refresh_status", func() {
		It("keep_alive", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-time.Minute))
			host.Inventory = ""
			mockConnectivityAndHwValidators(&host, mockHWValidator, mockConnectivityValidator, false, true, true)
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			expectedReply.expectedState = HostStatusKnown
		})
		It("keep_alive_timeout", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-time.Hour))
			mockConnectivityAndHwValidators(&host, mockHWValidator, mockConnectivityValidator, false, true, true)
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			expectedReply.expectedState = HostStatusDisconnected
		})
	})

	Context("install", func() {
		It("no_role", func() {
			host.Role = ""
			updateReply, updateErr = state.Install(ctx, &host, nil)
			expectedReply.expectError = true
		})
		It("with_role", func() {
			host.Role = "master"
			updateReply, updateErr = state.Install(ctx, &host, nil)
			expectedReply.expectedState = HostStatusInstalling
		})
		It("with_role_and_transaction", func() {
			tx := db.Begin()
			Expect(tx.Error).ShouldNot(HaveOccurred())
			host.Role = "master"
			updateReply, updateErr = state.Install(ctx, &host, tx)
			expectedReply = nil
			Expect(tx.Rollback().Error).ShouldNot(HaveOccurred())
			h := getHost(id, clusterId, db)
			Expect(swag.StringValue(h.Status)).Should(Equal(currentState))
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
