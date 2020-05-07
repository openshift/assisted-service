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

var _ = Describe("instructionmanager", func() {
	var (
		ctx               = context.Background()
		host              models.Host
		db                *gorm.DB
		stepsReply        models.Steps
		hostId, clusterId strfmt.UUID
		stepsErr          error
		instMng           *InstructionManager
		ctrl              *gomock.Controller
		mockValidator     *hardware.MockValidator
		instructionConfig InstructionConfig
	)

	BeforeEach(func() {
		db = prepareDB()
		ctrl = gomock.NewController(GinkgoT())
		mockValidator = hardware.NewMockValidator(ctrl)
		instMng = NewInstructionManager(getTestLog(), db, mockValidator, instructionConfig)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		cluster := models.Cluster{
			Base: models.Base{
				ID: &clusterId,
			},
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		host = models.Host{
			Base: models.Base{
				ID: &hostId,
			},
			ClusterID:    clusterId,
			Role:         RoleMaster,
			Status:       swag.String("unknown invalid state"),
			HardwareInfo: defaultHwInfo,
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	Context("get_next_steps", func() {
		It("invalid_host_state", func() {
			stepsReply, stepsErr = instMng.GetNextSteps(ctx, &host)
			Expect(stepsReply).To(HaveLen(0))
			Expect(stepsErr).Should(BeNil())
		})
		It("discovering", func() {
			checkStepsByState(HostStatusDiscovering, &host, db, instMng, mockValidator, ctx,
				[]models.StepType{models.StepTypeHardwareInfo, models.StepTypeConnectivityCheck})
		})
		It("known", func() {
			checkStepsByState(HostStatusKnown, &host, db, instMng, mockValidator, ctx,
				[]models.StepType{models.StepTypeConnectivityCheck})
		})
		It("disconnected", func() {
			checkStepsByState(HostStatusDisconnected, &host, db, instMng, mockValidator, ctx,
				[]models.StepType{models.StepTypeHardwareInfo, models.StepTypeConnectivityCheck})
		})
		It("insufficient", func() {
			checkStepsByState(HostStatusInsufficient, &host, db, instMng, mockValidator, ctx,
				[]models.StepType{models.StepTypeConnectivityCheck})
		})
		It("error", func() {
			checkStepsByState(HostStatusError, &host, db, instMng, mockValidator, ctx,
				[]models.StepType{})
		})
		It("installing", func() {
			checkStepsByState(HostStatusInstalling, &host, db, instMng, mockValidator, ctx,
				[]models.StepType{models.StepTypeExecute})
		})

	})

	AfterEach(func() {

		// cleanup
		db.Close()
		ctrl.Finish()
		stepsReply = nil
		stepsErr = nil
	})

})

func checkStepsByState(state string, host *models.Host, db *gorm.DB, instMng *InstructionManager, mockValidator *hardware.MockValidator, ctx context.Context,
	expectedStepTypes []models.StepType) {
	updateReply, updateErr := updateState(getTestLog(), state, "", host, db)
	Expect(updateErr).ShouldNot(HaveOccurred())
	Expect(updateReply.IsChanged).Should(BeTrue())
	h := getHost(*host.ID, host.ClusterID, db)
	Expect(swag.StringValue(h.Status)).Should(Equal(state))
	validDiskSize := int64(128849018880)
	var disks = []*models.BlockDevice{
		{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdb", Size: validDiskSize},
		{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sda", Size: validDiskSize},
		{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdh", Size: validDiskSize},
	}
	mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).AnyTimes()
	stepsReply, stepsErr := instMng.GetNextSteps(ctx, h)
	Expect(stepsReply).To(HaveLen(len(expectedStepTypes)))
	for i, step := range stepsReply {
		Expect(step.StepType).Should(Equal(expectedStepTypes[i]))
	}
	Expect(stepsErr).ShouldNot(HaveOccurred())
}
