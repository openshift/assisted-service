package host

import (
	"context"
	"time"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
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
		host = models.Host{
			Base: models.Base{
				ID: &id,
			},
			ClusterID:    clusterId,
			Status:       swag.String(currentState),
			HardwareInfo: defaultHwInfo,
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		expectedReply = &expect{expectedState: currentState}
	})

	Context("register_host", func() {
		It("already_exists_sufficient_hw", func() {
			updateReply, updateErr = state.RegisterHost(ctx, &host)
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.HardwareInfo).Should(Equal(""))
			}
		})
		It("new_host_sufficient_hardware", func() {
			Expect(db.Delete(&host).Error).ShouldNot(HaveOccurred())
			host.HardwareInfo = ""
			updateReply, updateErr = state.RegisterHost(ctx, &host)
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.HardwareInfo).Should(Equal(""))
			}
		})
	})

	Context("update_hw_info", func() {
		It("sufficient_hw", func() {
			mockValidator.EXPECT().IsSufficient(gomock.Any()).
				Return(&hardware.IsSufficientReply{IsSufficient: true}, nil).Times(1)
			updateReply, updateErr = state.UpdateHwInfo(ctx, &host, "some hw info")
			expectedReply.expectedState = HostStatusKnown
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.HardwareInfo).Should(Equal("some hw info"))
			}
		})
		It("insufficient_hw", func() {
			mockValidator.EXPECT().IsSufficient(gomock.Any()).
				Return(&hardware.IsSufficientReply{IsSufficient: false, Reason: "because"}, nil).Times(1)
			updateReply, updateErr = state.UpdateHwInfo(ctx, &host, "some hw info")
			expectedReply.expectedState = HostStatusInsufficient
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.HardwareInfo).Should(Equal("some hw info"))
				Expect(h.StatusInfo).Should(Equal("because"))
			}
		})
		It("hw_validation_error", func() {
			mockValidator.EXPECT().IsSufficient(gomock.Any()).
				Return(nil, errors.New("error")).Times(1)
			updateReply, updateErr = state.UpdateHwInfo(ctx, &host, "some hw info")
			expectedReply.expectError = true
			expectedReply.postCheck = func() {
				h := getHost(id, clusterId, db)
				Expect(h.HardwareInfo).Should(Equal(defaultHwInfo))
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
				Expect(h.Role).Should(Equal(""))
			}
		})
	})

	Context("refresh_status", func() {
		It("keep_alive", func() {
			updateReply, updateErr = state.RefreshStatus(ctx, &host)
		})
		It("keep_alive_timeout", func() {
			host.UpdatedAt = strfmt.DateTime(time.Now().Add(-time.Hour))
			updateReply, updateErr = state.RefreshStatus(ctx, &host)
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
