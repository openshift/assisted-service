package staticnetworkconfig

import (
	"encoding/json"
	"fmt"
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

var _ = Describe("validate mac interface mapping", func() {
	var (
		staticNetworkGenerator = StaticNetworkConfigGenerator{log: logrus.New(), nmstate: nmstate.New()}
		ethTemplate            = `[connection]
autoconnect=true
autoconnect-slaves=-1
id=%s
interface-name=%s
type=802-3-ethernet
uuid=dfd202f5-562f-5f07-8f2a-a7717756fb70

[ipv4]
address0=192.168.122.250/24
method=manual

[ipv6]
addr-gen-mode=0
address0=2001:db8::1:1/64
method=manual
`
		eth0Ini = fmt.Sprintf(ethTemplate, "eth0", "eth0")
		eth1Ini = fmt.Sprintf(ethTemplate, "eth1", "eth1")
	)
	It("one interface without mac-interface mapping", func() {
		err := staticNetworkGenerator.validateInterfaceNamesExistence(nil, []StaticNetworkConfigData{
			{
				FileContents: eth0Ini,
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("mac-interface mapping for interface"))
	})
	It("one interface with mac-interface mapping", func() {
		err := staticNetworkGenerator.validateInterfaceNamesExistence(models.MacInterfaceMap{
			{
				LogicalNicName: "eth0",
				MacAddress:     "f8:75:a4:a4:00:fe",
			},
		}, []StaticNetworkConfigData{
			{
				FileContents: eth0Ini,
			},
		})
		Expect(err).ToNot(HaveOccurred())
	})
	It("two interfaces. only one mac-interface mapping", func() {
		err := staticNetworkGenerator.validateInterfaceNamesExistence(models.MacInterfaceMap{
			{
				LogicalNicName: "eth0",
				MacAddress:     "f8:75:a4:a4:00:fe",
			},
		}, []StaticNetworkConfigData{
			{
				FileContents: eth0Ini,
			},
			{
				FileContents: eth1Ini,
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("mac-interface mapping for interface"))
	})
	It("two interfaces. with mac-interface mapping", func() {
		err := staticNetworkGenerator.validateInterfaceNamesExistence(models.MacInterfaceMap{
			{
				LogicalNicName: "eth0",
				MacAddress:     "f8:75:a4:a4:00:fe",
			},
			{
				LogicalNicName: "eth1",
				MacAddress:     "f8:75:a4:a4:00:ff",
			},
		}, []StaticNetworkConfigData{
			{
				FileContents: eth0Ini,
			},
			{
				FileContents: eth1Ini,
			},
		})
		Expect(err).ToNot(HaveOccurred())
	})
	Context("bond with 2 ports", func() {
		bondConnection := `[connection]
autoconnect=true
autoconnect-slaves=1
id=bond99
interface-name=bond99
type=bond
uuid=4a920503-4862-5505-80fd-4738d07f44c6

[bond]
miimon=140
mode=balance-rr

[ipv4]
address0=192.0.2.0/24
method=manual

[ipv6]
method=disabled
`
		eth2Connection := `[connection]
autoconnect=true
autoconnect-slaves=-1
id=eth2
interface-name=eth2
master=4a920503-4862-5505-80fd-4738d07f44c6
slave-type=bond
type=802-3-ethernet
uuid=21373057-f376-5091-afb6-64de925c23ed
`
		eth3Connection := `[connection]
autoconnect=true
autoconnect-slaves=-1
id=eth3
interface-name=eth3
master=4a920503-4862-5505-80fd-4738d07f44c6
slave-type=bond
type=802-3-ethernet
uuid=7e211aea-3d14-59cf-a4fa-be91dac5dbba
`
		It("wrong interface names mapping", func() {
			err := staticNetworkGenerator.validateInterfaceNamesExistence(models.MacInterfaceMap{
				{
					LogicalNicName: "eth0",
					MacAddress:     "f8:75:a4:a4:00:fe",
				},
				{
					LogicalNicName: "eth1",
					MacAddress:     "f8:75:a4:a4:00:ff",
				},
			}, []StaticNetworkConfigData{
				{
					FileContents: bondConnection,
				},
				{
					FileContents: eth2Connection,
				},
				{
					FileContents: eth3Connection,
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("mac-interface mapping for interface"))
		})
		It("correct interface names mapping", func() {
			err := staticNetworkGenerator.validateInterfaceNamesExistence(models.MacInterfaceMap{
				{
					LogicalNicName: "eth2",
					MacAddress:     "f8:75:a4:a4:00:fe",
				},
				{
					LogicalNicName: "eth3",
					MacAddress:     "f8:75:a4:a4:00:ff",
				},
			}, []StaticNetworkConfigData{
				{
					FileContents: bondConnection,
				},
				{
					FileContents: eth2Connection,
				},
				{
					FileContents: eth3Connection,
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})
	})
	Context("vlan", func() {
		vlanConnection := `[connection]
autoconnect=true
autoconnect-slaves=-1
id=eth1.101
interface-name=eth1.101
type=vlan
uuid=6d0f1528-fd9c-52a9-9aa5-b825aa883bc3

[ipv4]
method=disabled

[ipv6]
method=disabled

[vlan]
id=101
parent=eth1
`
		It("vlan only - no underlying interface", func() {
			err := staticNetworkGenerator.validateInterfaceNamesExistence(nil, []StaticNetworkConfigData{
				{
					FileContents: vlanConnection,
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no mac address mapping can be associated with the available network interfaces"))
		})
		It("vlan with underlying interface - no mapping", func() {
			err := staticNetworkGenerator.validateInterfaceNamesExistence(nil, []StaticNetworkConfigData{
				{
					FileContents: vlanConnection,
				},
				{
					FileContents: eth1Ini,
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("mac-interface mapping for interface"))
		})
		It("vlan with underlying interface - with mapping", func() {
			err := staticNetworkGenerator.validateInterfaceNamesExistence(models.MacInterfaceMap{
				{
					LogicalNicName: "eth1",
					MacAddress:     "f8:75:a4:a4:00:fe",
				},
			}, []StaticNetworkConfigData{
				{
					FileContents: vlanConnection,
				},
				{
					FileContents: eth1Ini,
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})
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
