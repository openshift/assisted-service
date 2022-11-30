package staticnetworkconfig

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/nmstate/nmstate/rust/src/go/nmstate"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

func TestStaticNetworkConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "StaticNetworkConfig Suite")
}

var _ = Describe("generateConfiguration", func() {
	var (
		staticNetworkGenerator = StaticNetworkConfigGenerator{log: logrus.New(), nmstate: nmstate.New()}
	)

	It("Fail with an empty host YAML", func() {
		_, err := staticNetworkGenerator.generateConfiguration("")
		Expect(err).To(HaveOccurred())
	})

	It("Fail with an invalid host YAML", func() {
		_, err := staticNetworkGenerator.generateConfiguration("interfaces:\n    - foo: badConfig")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("InvalidArgument"))
	})

	It("Success", func() {
		hostYaml := `interfaces:
- ipv4:
    address:
    - ip: 192.0.2.1
      prefix-length: 24
    dhcp: false
    enabled: true
  name: eth0
  state: up
  type: ethernet`
		config, err := staticNetworkGenerator.generateConfiguration(hostYaml)
		Expect(err).NotTo(HaveOccurred())
		Expect(config).To(ContainSubstring("address0=192.0.2.1/24"))
	})
})

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

	It("check formatting static network for DB", func() {
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
		expectedOutputAsBytes, err := json.Marshal(&staticNetworkConfig)
		Expect(err).ToNot(HaveOccurred())
		formattedOutput, err := staticNetworkGenerator.FormatStaticNetworkConfigForDB(staticNetworkConfig)
		Expect(err).ToNot(HaveOccurred())
		Expect(formattedOutput).To(Equal(string(expectedOutputAsBytes)))
	})

	It("sorted formatting static network for DB", func() {
		map1 := models.MacInterfaceMap{
			&models.MacInterfaceMapItems0{MacAddress: "mac10", LogicalNicName: "nic10"},
			&models.MacInterfaceMapItems0{MacAddress: "mac0", LogicalNicName: "nic0"},
		}
		sortedMap1 := models.MacInterfaceMap{
			&models.MacInterfaceMapItems0{MacAddress: "mac0", LogicalNicName: "nic0"},
			&models.MacInterfaceMapItems0{MacAddress: "mac10", LogicalNicName: "nic10"},
		}
		map2 := models.MacInterfaceMap{
			&models.MacInterfaceMapItems0{MacAddress: "mac20", LogicalNicName: "nic20"},
		}
		unsortedStaticNetworkConfig := []*models.HostStaticNetworkConfig{
			common.FormatStaticConfigHostYAML("nic20", "02000048ba48", "192.168.126.31", "192.168.141.31", "192.168.126.1", map2),
			common.FormatStaticConfigHostYAML("nic10", "02000048ba38", "192.168.126.30", "192.168.141.30", "192.168.126.1", map1),
		}
		sortedStaticNetworkConfig := []*models.HostStaticNetworkConfig{
			common.FormatStaticConfigHostYAML("nic10", "02000048ba38", "192.168.126.30", "192.168.141.30", "192.168.126.1", sortedMap1),
			common.FormatStaticConfigHostYAML("nic20", "02000048ba48", "192.168.126.31", "192.168.141.31", "192.168.126.1", map2),
		}

		unexpectedOutputAsBytes, err := json.Marshal(unsortedStaticNetworkConfig)
		Expect(err).ToNot(HaveOccurred())
		formattedOutput, err := staticNetworkGenerator.FormatStaticNetworkConfigForDB(unsortedStaticNetworkConfig)
		Expect(err).ToNot(HaveOccurred())
		Expect(formattedOutput).ToNot(Equal(string(unexpectedOutputAsBytes)))
		expectedOutputAsBytes, err := json.Marshal(sortedStaticNetworkConfig)
		Expect(err).ToNot(HaveOccurred())
		Expect(formattedOutput).To(Equal(string(expectedOutputAsBytes)))
	})

	It("check empty formatting static network for DB", func() {
		formattedOutput, err := staticNetworkGenerator.FormatStaticNetworkConfigForDB(nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(formattedOutput).To(Equal(""))
	})
})

var _ = Describe("StaticNetworkConfig.GenerateStaticNetworkConfigArchive", func() {
	It("successfully produces an archive with one host data", func() {
		data := []StaticNetworkConfigData{
			{
				FilePath:     "host1",
				FileContents: "static network config data of first host",
			},
		}
		archiveBytes, err := GenerateStaticNetworkConfigArchive(data)
		Expect(err).ToNot(HaveOccurred())
		Expect(archiveBytes).ToNot(BeNil())
		checkArchiveString(archiveBytes.String(), data)
	})
	It("successfully produces an archive when file contents is empty", func() {
		data := []StaticNetworkConfigData{
			{
				FilePath: "host1",
			},
		}
		archiveBytes, err := GenerateStaticNetworkConfigArchive(data)
		Expect(err).ToNot(HaveOccurred())
		Expect(archiveBytes).ToNot(BeNil())
		checkArchiveString(archiveBytes.String(), data)
	})
	It("successfully produces an archive with multiple hosts' data", func() {
		data := []StaticNetworkConfigData{
			{
				FilePath:     "host1",
				FileContents: "static network config data of first host",
			},
			{
				FilePath:     "host2",
				FileContents: "static network config data of second host",
			},
		}
		archiveBytes, err := GenerateStaticNetworkConfigArchive(data)
		Expect(err).ToNot(HaveOccurred())
		Expect(archiveBytes).ToNot(BeNil())
		checkArchiveString(archiveBytes.String(), data)
	})
})

func checkArchiveString(archiveString string, allData []StaticNetworkConfigData) {
	for _, data := range allData {
		Expect(archiveString).To(ContainSubstring("tar"))
		Expect(archiveString).To(ContainSubstring(filepath.Join("/etc/assisted/network", data.FilePath)))
		Expect(archiveString).To(ContainSubstring(data.FileContents))
	}
}
