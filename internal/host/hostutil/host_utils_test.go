package hostutil

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ValidateInstallerArgs", func() {
	It("Parses correctly", func() {
		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n", "--save-partindex", "1", "--image-url", "https://example.com/image"}
		err := ValidateInstallerArgs(args)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Denies unexpected arguments", func() {
		args := []string{"--not-supported", "value"}
		err := ValidateInstallerArgs(args)
		Expect(err).To(HaveOccurred())
	})

	It("Succeeds with an empty list", func() {
		err := ValidateInstallerArgs([]string{})
		Expect(err).NotTo(HaveOccurred())
	})
})

func TestHostUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HostUtil Tests")
}
