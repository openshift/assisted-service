package staticnetworkconfig

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	models "github.com/openshift/assisted-service/models/v1"
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

	It("validate mac interface", func() {
		input := models.MacInterfaceMap{
			{LogicalNicName: "eth0", MacAddress: "macaddress0"},
			{LogicalNicName: "eth1", MacAddress: "macaddress1"},
			{LogicalNicName: "eth2", MacAddress: "macaddress2"},
		}
		err := staticNetworkGenerator.validateMacInterfaceName(0, input)
		Expect(err).ToNot(HaveOccurred())

		input = models.MacInterfaceMap{
			{LogicalNicName: "eth0", MacAddress: "macaddress0"},
			{LogicalNicName: "eth1", MacAddress: "macaddress1"},
			{LogicalNicName: "eth0", MacAddress: "macaddress2"},
		}
		err = staticNetworkGenerator.validateMacInterfaceName(0, input)
		Expect(err).To(HaveOccurred())

		input = models.MacInterfaceMap{
			{LogicalNicName: "eth0", MacAddress: "macaddress0"},
			{LogicalNicName: "eth1", MacAddress: "macaddress1"},
			{LogicalNicName: "eth2", MacAddress: "macaddress0"},
		}
		err = staticNetworkGenerator.validateMacInterfaceName(0, input)
		Expect(err).To(HaveOccurred())
	})

	It("check formating static network for DB", func() {
		map1 := models.MacInterfaceMap{
			&models.MacInterfaceMapItems0{MacAddress: "mac10", LogicalNicName: "nic10"},
		}
		map2 := models.MacInterfaceMap{
			&models.MacInterfaceMapItems0{MacAddress: "mac20", LogicalNicName: "nic20"},
		}
		staticNetworkConfig := []*models.HostStaticNetworkConfig{
			common.FormatStaticConfigHostYAML("nic10", "02000048ba38", "192.168.126.30", "192.168.141.30", "192.168.126.1", map1),
			common.FormatStaticConfigHostYAML("nic20", "02000048ba48", "192.168.126.31", "192.168.141.31", "192.168.126.1", map2),
		}
		expectedOutput := staticNetworkConfig[0].NetworkYaml + hostStaticNetworkDelimeter + "mac10=nic10" + staticNetworkConfigHostsDelimeter + staticNetworkConfig[1].NetworkYaml + hostStaticNetworkDelimeter + "mac20=nic20"
		formattedOutput := staticNetworkGenerator.FormatStaticNetworkConfigForDB(staticNetworkConfig)
		Expect(formattedOutput).To(Equal(expectedOutput))
	})

	It("check empty formating static network for DB", func() {
		formattedOutput := staticNetworkGenerator.FormatStaticNetworkConfigForDB(nil)
		Expect(formattedOutput).To(Equal(""))
	})
})
