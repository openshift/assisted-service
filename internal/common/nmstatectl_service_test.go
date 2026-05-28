package common

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("FormatMinimalISONetworkConfigServiceNmstatectl", func() {
	It("embeds discovery delay and timeout in the systemd unit", func() {
		content, err := FormatMinimalISONetworkConfigServiceNmstatectl(5)
		Expect(err).ToNot(HaveOccurred())
		Expect(content).To(ContainSubstring("Environment=DISCOVERY_DELAY_SECONDS=5"))
		Expect(content).To(ContainSubstring("TimeoutSec=65"))
	})
})
