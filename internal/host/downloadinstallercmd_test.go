package host

import (
	"context"
	"fmt"

	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("downloadInstallerCmd", func() {
	var (
		ctx       = context.Background()
		host      models.Host
		invCmd    *downloadInstallerCmd
		stepReply *models.Step
		stepErr   error
		cfg       InstructionConfig
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		invCmd = NewDownloadInstallerCmd(getTestLog(), cfg)
	})

	It("get_step", func() {
		stepReply, stepErr = invCmd.GetStep(ctx, &host)
		Expect(stepReply.StepType).To(Equal(models.StepTypeExecute))
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(stepReply.Command).Should(Equal("timeout"))
		expectedArgs := []string{"15m", "bash", "-c",
			fmt.Sprintf("until podman pull %s; do sleep 1; done", cfg.InstallerImage)}
		Expect(stepReply.Args).Should(Equal(expectedArgs))
	})
})
