package host

import (
	"context"

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

var _ = Describe("statemachine", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		ctrl          *gomock.Controller
		mockValidator *hardware.MockValidator
		state         API
		host          models.Host
		stateReply    *UpdateReply
		stateErr      error
	)

	BeforeEach(func() {
		db = prepareDB()
		ctrl = gomock.NewController(GinkgoT())
		mockValidator = hardware.NewMockValidator(ctrl)
		state = NewState(getTestLog(), db, mockValidator)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		host = models.Host{
			Base: models.Base{
				ID: &id,
			},
			ClusterID:    clusterId,
			Status:       swag.String("unknown invalid state"),
			HardwareInfo: defaultHwInfo,
		}
	})

	Context("unknown_host_state", func() {
		It("register_host", func() {
			stateReply, stateErr = state.RegisterHost(ctx, &host)
		})

		It("enable_host", func() {
			stateReply, stateErr = state.EnableHost(ctx, &host)
		})

		It("disable_host", func() {
			stateReply, stateErr = state.DisableHost(ctx, &host)
		})

		It("update role", func() {
			stateReply, stateErr = state.UpdateRole(ctx, &host, "master", nil)
		})

		It("install", func() {
			stateReply, stateErr = state.Install(ctx, &host, nil)
		})

		It("update_hw_info", func() {
			stateReply, stateErr = state.UpdateHwInfo(ctx, &host, "some hw info")
		})

		It("update_hw_info", func() {
			stateReply, stateErr = state.RefreshStatus(ctx, &host)
		})

		AfterEach(func() {
			Expect(stateReply).To(BeNil())
			Expect(stateErr).Should(HaveOccurred())
		})
	})

	AfterEach(func() {
		ctrl.Finish()
		db.Close()
	})
})
