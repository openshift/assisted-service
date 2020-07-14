package host

import (
	"context"
	"testing"

	"github.com/filanov/bm-inventory/internal/common"
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
		hwValidator       *hardware.MockValidator
		instructionConfig InstructionConfig
	)

	BeforeEach(func() {
		db = prepareDB()
		ctrl = gomock.NewController(GinkgoT())
		hwValidator = hardware.NewMockValidator(ctrl)
		instMng = NewInstructionManager(getTestLog(), db, hwValidator, instructionConfig, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{Cluster: models.Cluster{ID: &clusterId}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		host = getTestHost(hostId, clusterId, "unknown invalid state")
		host.Role = models.HostRoleMaster
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	Context("get_next_steps", func() {
		It("invalid_host_state", func() {
			stepsReply, stepsErr = instMng.GetNextSteps(ctx, &host)
			Expect(stepsReply.Instructions).To(HaveLen(0))
			Expect(stepsErr).Should(BeNil())
		})
		It("discovering", func() {
			checkStepsByState(HostStatusDiscovering, &host, db, instMng, hwValidator, ctx,
				[]models.StepType{models.StepTypeHardwareInfo, models.StepTypeInventory, models.StepTypeConnectivityCheck})
		})
		It("known", func() {

			checkStepsByState(HostStatusKnown, &host, db, instMng, hwValidator, ctx,
				[]models.StepType{models.StepTypeConnectivityCheck, models.StepTypeFreeNetworkAddresses})
		})
		It("disconnected", func() {
			checkStepsByState(HostStatusDisconnected, &host, db, instMng, hwValidator, ctx,
				[]models.StepType{models.StepTypeHardwareInfo, models.StepTypeInventory, models.StepTypeConnectivityCheck})
		})
		It("insufficient", func() {
			checkStepsByState(HostStatusInsufficient, &host, db, instMng, hwValidator, ctx,
				[]models.StepType{models.StepTypeHardwareInfo, models.StepTypeInventory, models.StepTypeConnectivityCheck, models.StepTypeFreeNetworkAddresses})
		})
		It("error", func() {
			checkStepsByState(HostStatusError, &host, db, instMng, hwValidator, ctx,
				[]models.StepType{models.StepTypeExecute})
		})
		It("installing", func() {
			checkStepsByState(HostStatusInstalling, &host, db, instMng, hwValidator, ctx,
				[]models.StepType{models.StepTypeInstall})
		})
		It("reset", func() {
			checkStepsByState(HostStatusResetting, &host, db, instMng, hwValidator, ctx,
				[]models.StepType{models.StepTypeResetInstallation})
		})
	})

	AfterEach(func() {
		// cleanup
		db.Close()
		ctrl.Finish()
		stepsReply = models.Steps{}
		stepsErr = nil
	})

})

func checkStepsByState(state string, host *models.Host, db *gorm.DB, instMng *InstructionManager, mockValidator *hardware.MockValidator, ctx context.Context,
	expectedStepTypes []models.StepType) {
	updateReply, updateErr := updateHostState(getTestLog(), state, "", host, db)
	ExpectWithOffset(1, updateErr).ShouldNot(HaveOccurred())
	ExpectWithOffset(1, updateReply.IsChanged).Should(BeTrue())
	h := getHost(*host.ID, host.ClusterID, db)
	ExpectWithOffset(1, swag.StringValue(h.Status)).Should(Equal(state))
	validDiskSize := int64(128849018880)
	var disks = []*models.Disk{
		{DriveType: "disk", Name: "sdb", SizeBytes: validDiskSize},
		{DriveType: "disk", Name: "sda", SizeBytes: validDiskSize},
		{DriveType: "disk", Name: "sdh", SizeBytes: validDiskSize},
	}
	mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).AnyTimes()
	stepsReply, stepsErr := instMng.GetNextSteps(ctx, h)
	ExpectWithOffset(1, stepsReply.Instructions).To(HaveLen(len(expectedStepTypes)))
	if stateValues, ok := instMng.stateToSteps[state]; ok {
		Expect(stepsReply.NextInstructionSeconds).Should(Equal(stateValues.NextStepInSec))
	} else {
		Expect(stepsReply.NextInstructionSeconds).Should(Equal(defaultNextInstructionInSec))
	}

	for i, step := range stepsReply.Instructions {
		ExpectWithOffset(1, step.StepType).Should(Equal(expectedStepTypes[i]))
	}
	ExpectWithOffset(1, stepsErr).ShouldNot(HaveOccurred())
}

func TestInstructionManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "instruction manager tests")
}
