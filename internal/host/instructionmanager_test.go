package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("instructionmanager", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var stepsReply models.Steps
	var id, clusterId strfmt.UUID
	var stepsErr error
	var instMng *InstructionManager

	BeforeEach(func() {
		db = prepareDB()
		instMng = NewInstructionManager(getTestLog(), db)
		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = models.Host{
			Base: models.Base{
				ID: &id,
			},
			ClusterID:    clusterId,
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
			checkStepsByState(HostStatusDiscovering, &host, db, instMng, ctx,
				[]models.StepType{models.StepTypeHardwareInfo, models.StepTypeConnectivityCheck})
		})
		It("known", func() {
			checkStepsByState(HostStatusKnown, &host, db, instMng, ctx,
				[]models.StepType{models.StepTypeConnectivityCheck})
		})
		It("disconnected", func() {
			checkStepsByState(HostStatusDisconnected, &host, db, instMng, ctx,
				[]models.StepType{models.StepTypeHardwareInfo, models.StepTypeConnectivityCheck})
		})
		It("insufficient", func() {
			checkStepsByState(HostStatusInsufficient, &host, db, instMng, ctx,
				[]models.StepType{models.StepTypeConnectivityCheck})
		})
		It("error", func() {
			checkStepsByState(HostStatusError, &host, db, instMng, ctx,
				[]models.StepType{})
		})

	})

	AfterEach(func() {

		// cleanup
		db.Close()
		stepsReply = nil
		stepsErr = nil
	})

})

func checkStepsByState(state string, host *models.Host, db *gorm.DB, instMng *InstructionManager, ctx context.Context,
	expectedStepTypes []models.StepType) {
	updateReply, updateErr := updateState(getTestLog(), state, "", host, db)
	Expect(updateErr).ShouldNot(HaveOccurred())
	Expect(updateReply.IsChanged).Should(BeTrue())
	h := getHost(*host.ID, host.ClusterID, db)
	Expect(swag.StringValue(h.Status)).Should(Equal(state))
	stepsReply, stepsErr := instMng.GetNextSteps(ctx, h)
	Expect(stepsReply).To(HaveLen(len(expectedStepTypes)))
	for i, step := range stepsReply {
		Expect(step.StepType).Should(Equal(expectedStepTypes[i]))
	}
	Expect(stepsErr).ShouldNot(HaveOccurred())
}
