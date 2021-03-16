package staticnetworkconfig

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

func TestStaticNetworkConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "StaticNetworkConfig Suite")
}

var _ = Describe("StaticNetworkConfig", func() {
	var (
		staticNetworkGenerator = StaticNetworkConfigGenerator{log: logrus.New()}
	)

	It("hw interface name validation", func() {
		mac, err := staticNetworkGenerator.validateAndCalculateMAC("09230fd892AA")
		Expect(err).ToNot(HaveOccurred())
		Expect(mac).To(Equal("09:23:0f:d8:92:AA"))

		mac, err = staticNetworkGenerator.validateAndCalculateMAC("09230fd892AA89")
		Expect(err).To(HaveOccurred())
		Expect(mac).To(Equal(""))

		mac, err = staticNetworkGenerator.validateAndCalculateMAC("09230Gd892AA")
		Expect(err).To(HaveOccurred())
		Expect(mac).To(Equal(""))

		mac, err = staticNetworkGenerator.validateAndCalculateMAC("09230d892AA")
		Expect(err).To(HaveOccurred())
		Expect(mac).To(Equal(""))
	})
})
