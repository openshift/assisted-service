package hostcommands

import (
	"fmt"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

var _ = Describe("Format command for starting next step agent", func() {

	var config NextStepRunnerConfig

	infraEnvId := uuid.New().String()
	hostID := uuid.New().String()
	serviceURL := uuid.New().String()
	image := uuid.New().String()
	certVolume := fmt.Sprintf("%s:%s", common.HostCACertPath, common.HostCACertPath)

	BeforeEach(func() {
		config = NextStepRunnerConfig{
			InfraEnvID:          infraEnvId,
			HostID:              hostID,
			NextStepRunnerImage: image,
		}
	})

	It("standard formatting", func() {
		config.ServiceBaseURL = serviceURL
		command, args := GetNextStepRunnerCommand(&config)
		Expect(command).Should(Equal("podman"))
		assertValue("--infra-env-id", infraEnvId, *args)
		assertValue("--host-id", hostID, *args)
		assertValue("--url", serviceURL, *args)
		assertValue("--agent-version", image, *args)
		Expect(*args).ShouldNot(ContainElement("--cacert"))
		Expect(*args).ShouldNot(ContainElement(certVolume))
		Expect(*args).Should(ContainElement("/etc/pki:/etc/pki"))
		Expect(*args).Should(ContainElement("--insecure=false"))
		Expect(*args).ShouldNot(ContainElement("--insecure=true"))
	})

	It("trim service URL", func() {
		config.ServiceBaseURL = fmt.Sprintf(" %s ", serviceURL)
		Expect(config.ServiceBaseURL).ShouldNot(Equal(serviceURL))
		_, args := GetNextStepRunnerCommand(&config)
		assertValue("--url", serviceURL, *args)
	})

	It("without custom CA certificate", func() {
		config.UseCustomCACert = false
		_, args := GetNextStepRunnerCommand(&config)

		Expect(*args).ShouldNot(ContainElement("--cacert"))
		Expect(*args).ShouldNot(ContainElement(certVolume))
	})

	It("with custom CA certificate", func() {
		config.UseCustomCACert = true
		_, args := GetNextStepRunnerCommand(&config)
		assertValue("--cacert", common.HostCACertPath, *args)
		Expect(*args).Should(ContainElement(certVolume))
	})

	It("certificate verification on", func() {
		config.SkipCertVerification = false
		_, args := GetNextStepRunnerCommand(&config)
		Expect(*args).Should(ContainElement("--insecure=false"))
		Expect(*args).ShouldNot(ContainElement("--insecure=true"))
	})

	It("certificate verification off", func() {
		config.SkipCertVerification = true
		_, args := GetNextStepRunnerCommand(&config)
		Expect(*args).Should(ContainElement("--insecure=true"))
		Expect(*args).ShouldNot(ContainElement("--insecure=false"))
	})
})

func assertValue(key string, value string, args []string) {
	i := search(key, args)
	Expect(i).Should(BeNumerically(">", -1))
	Expect(i).Should(BeNumerically("<", len(args)-1))
	Expect(args[i+1]).Should(Equal(value))
}

func search(term string, s []string) int {

	for i, v := range s {
		if v == term {
			return i
		}
	}

	return -1
}
