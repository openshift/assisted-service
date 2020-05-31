package host

import (
	"context"
	"fmt"
	"time"

	"github.com/go-openapi/swag"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
)

var _ = Describe("discovering_state", func() {
	var (
		ctx           = context.Background()
		state         API
		db            *gorm.DB
		currentState  = HostStatusDiscovering
		host          models.Host
		id, clusterId strfmt.UUID
		updateReply   *UpdateReply
		updateErr     error
		expectedReply *expect
		ctrl          *gomock.Controller
		mockValidator *hardware.MockValidator
	)

	BeforeEach(func() {
		db = prepareDB()
		ctrl = gomock.NewController(GinkgoT())
		mockValidator = hardware.NewMockValidator(ctrl)
		state = &Manager{discovering: NewDiscoveringState(getTestLog(), db, mockValidator)}

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, currentState)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		expectedReply = &expect{expectedState: currentState}
		addTestCluster(clusterId, "1.2.3.5", "1.2.3.6", "1.2.3.0/24", db)
	})

	Context("update inventory", func() {
		It("sufficient_hw", func() {
			mockValidator.EXPECT().IsSufficient(gomock.Any(), gomock.Any()).
				Return(&hardware.IsSufficientReply{IsSufficient: true}, nil).Times(1)
			updateReply, updateErr = state.UpdateInventory(ctx, &host, "some hw info")
			expectedReply.expectedState = HostStatusKnown
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.Inventory).Should(Equal("some hw info"))
			}
		})
		It("insufficient_hw", func() {
			mockValidator.EXPECT().IsSufficient(gomock.Any(), gomock.Any()).
				Return(&hardware.IsSufficientReply{IsSufficient: false, Reason: "because"}, nil).Times(1)
			updateReply, updateErr = state.UpdateInventory(ctx, &host, "some hw info")
			expectedReply.expectedState = HostStatusInsufficient
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.Inventory).Should(Equal("some hw info"))
				Expect(*h.StatusInfo).Should(Equal("because"))
			}
		})
		It("hw_validation_error", func() {
			mockValidator.EXPECT().IsSufficient(gomock.Any(), gomock.Any()).
				Return(nil, errors.New("error")).Times(1)
			updateReply, updateErr = state.UpdateInventory(ctx, &host, "some hw info")
			expectedReply.expectError = true
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.Inventory).Should(Equal(defaultInventory()))
			}
		})
	})
	Context("refresh status", func() {
		It("sufficient_hw", func() {
			expectedReply.postCheck = nil
			expectedReply.expectedState = "known"
			mockValidator.EXPECT().IsSufficient(gomock.Any(), gomock.Any()).
				Return(&hardware.IsSufficientReply{IsSufficient: true}, nil).Times(1)
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			Expect(updateErr).To(Not(HaveOccurred()))
			var h models.Host
			Expect(db.Take(&h, "id = ?", *host.ID).Error).NotTo(HaveOccurred())
			Expect(h.Status).To(Equal(swag.String("known")))
		})
		It("insufficient_hw", func() {
			expectedReply.postCheck = nil
			expectedReply.expectedState = "insufficient"
			mockValidator.EXPECT().IsSufficient(gomock.Any(), gomock.Any()).
				Return(&hardware.IsSufficientReply{IsSufficient: false}, nil).Times(1)
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			Expect(updateErr).To(Not(HaveOccurred()))
			var h models.Host
			Expect(db.Take(&h, "id = ?", *host.ID).Error).NotTo(HaveOccurred())
			Expect(h.Status).To(Equal(swag.String("insufficient")))
		})
		It("error", func() {
			expectedReply.postCheck = nil
			expectedReply.expectError = true
			mockValidator.EXPECT().IsSufficient(gomock.Any(), gomock.Any()).
				Return(nil, fmt.Errorf("Blah")).Times(1)
			updateReply, updateErr = state.RefreshStatus(ctx, &host, db)
			Expect(updateErr).To(HaveOccurred())
			var h models.Host
			Expect(db.Take(&h, "id = ?", *host.ID).Error).NotTo(HaveOccurred())
			Expect(h.Status).To(Equal(swag.String("discovering")))
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
				Expect(h.Role).Should(Equal(""))
			}
		})
	})

	Context("refresh_status", func() {
		It("keep_alive", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-time.Minute))
			updateReply, updateErr = state.RefreshStatus(ctx, &host, nil)
			expectedReply.expectedState = HostStatusDiscovering
		})
		It("keep_alive_timeout", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-time.Hour))
			host.Inventory = ""
			updateReply, updateErr = state.RefreshStatus(ctx, &host, nil)
		})
		It("keep_alive_timeout", func() {
			host.UpdatedAt = strfmt.DateTime(time.Now().Add(-time.Hour))
			updateReply, updateErr = state.RefreshStatus(ctx, &host, nil)
			expectedReply.expectedState = HostStatusDisconnected
		})
	})

	It("install", func() {
		updateReply, updateErr = state.Install(ctx, &host, nil)
		expectedReply.expectError = true
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
