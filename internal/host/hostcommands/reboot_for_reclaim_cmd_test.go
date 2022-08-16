package hostcommands

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("reboot_for_reclaim_cmd.GetSteps", func() {
	var (
		ctx                 = context.Background()
		host                models.Host
		rebootForReclaimCmd *rebootForReclaimCmd
		id                  strfmt.UUID
		infraEnvId          strfmt.UUID
		stepReply           []*models.Step
		stepErr             error
		hostFSMountDir      string
	)

	BeforeEach(func() {
		hostFSMountDir = "/host"
		rebootForReclaimCmd = NewRebootForReclaimCmd(common.GetTestLog(), hostFSMountDir)

		id = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHostWithInfraEnv(id, infraEnvId, models.HostStatusReclaimingRebooting, models.HostRoleWorker)
	})

	It("returns a request with the correct content", func() {
		stepReply, stepErr = rebootForReclaimCmd.GetSteps(ctx, &host)
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeRebootForReclaim))
		Expect(stepErr).To(BeNil())
	})

	AfterEach(func() {
		// cleanup
		stepReply = nil
		stepErr = nil
	})
})
