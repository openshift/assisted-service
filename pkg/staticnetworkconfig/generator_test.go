package staticnetworkconfig_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	snc "github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/sirupsen/logrus"
)

func TestStaticNetworkConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "StaticNetworkConfig Suite")
}

var _ = Describe("StaticNetworkConfig.GenerateStaticNetworkConfigData - generateConfiguration", func() {
	var (
		staticNetworkGenerator = snc.New(logrus.New())
	)

	It("Fail with an empty host YAML", func() {
		_, err := staticNetworkGenerator.GenerateStaticNetworkConfigData(context.Background(), `[{"network_yaml": ""}]`)
		Expect(err).To(HaveOccurred())
	})

	It("Fail with an invalid host YAML", func() {
		_, err := staticNetworkGenerator.GenerateStaticNetworkConfigData(context.Background(), `[{"network_yaml": "interfaces:\n    - foo: badConfig"}]`)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("InvalidArgument"))
	})

	It("Success", func() {
		var (
			hostsYAML = `[{ "network_yaml": "%s" }]`
			hostYAML  = `interfaces:
- name: eth0
  type: ethernet
  state: up
  ipv4:
    enabled: true
    address:
      - ip: 192.0.2.1
        prefix-length: 24`
		)
		escapedYamlContent, err := escapeYAMLForJSON(hostYAML)
		Expect(err).NotTo(HaveOccurred())
		config, err := staticNetworkGenerator.GenerateStaticNetworkConfigData(context.Background(), fmt.Sprintf(hostsYAML, escapedYamlContent))
		fileContent := config[0].FileContents
		Expect(err).NotTo(HaveOccurred())
		Expect(fileContent).To(ContainSubstring("address0=192.0.2.1/24"))
	})
})

var _ = Describe("StaticNetworkConfig.GenerateStaticNetworkConfigData - validate mac interface mapping", func() {
	var (
		staticNetworkGenerator = snc.New(logrus.New())
		singleInterfaceYAML    = `interfaces:
  - name: eth0
    type: ethernet
    state: up
    ipv4:
      enabled: true
      dhcp: false
      address:
        - ip: 192.0.2.1
          prefix-length: 24`

		multipleInterfacesYAML = `interfaces:
  - name: eth0
    type: ethernet
    state: up
    ipv4:
      enabled: true
      dhcp: false
      address:
        - ip: 192.0.2.1
          prefix-length: 24
  - name: eth1
    type: ethernet
    state: up
    ipv4:
      enabled: true
      dhcp: false
      address:
        - ip: 192.0.2.2
          prefix-length: 24`
	)
	It("one interface without mac-interface mapping", func() {
		err := staticNetworkGenerator.ValidateStaticConfigParams([]*models.HostStaticNetworkConfig{
			{
				MacInterfaceMap: []*models.MacInterfaceMapItems0{},
				NetworkYaml:     singleInterfaceYAML,
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("mac-interface mapping for interface"))
	})
	It("one interface with mac-interface mapping", func() {
		err := staticNetworkGenerator.ValidateStaticConfigParams([]*models.HostStaticNetworkConfig{
			{
				MacInterfaceMap: []*models.MacInterfaceMapItems0{
					{
						LogicalNicName: "eth0",
						MacAddress:     "f8:75:a4:a4:00:fe",
					},
				},
				NetworkYaml: singleInterfaceYAML,
			},
		})
		Expect(err).ToNot(HaveOccurred())
	})

	It("two interfaces. only one mac-interface mapping", func() {
		err := staticNetworkGenerator.ValidateStaticConfigParams([]*models.HostStaticNetworkConfig{
			{
				MacInterfaceMap: []*models.MacInterfaceMapItems0{
					{
						LogicalNicName: "eth0",
						MacAddress:     "f8:75:a4:a4:00:fe",
					},
				},
				NetworkYaml: multipleInterfacesYAML,
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("mac-interface mapping for interface"))
	})
	It("two interfaces. with mac-interface mapping", func() {
		err := staticNetworkGenerator.ValidateStaticConfigParams([]*models.HostStaticNetworkConfig{
			{
				MacInterfaceMap: []*models.MacInterfaceMapItems0{
					{
						LogicalNicName: "eth0",
						MacAddress:     "f8:75:a4:a4:00:fe",
					},
					{
						LogicalNicName: "eth1",
						MacAddress:     "f8:75:a4:a4:00:ff",
					},
				},
				NetworkYaml: multipleInterfacesYAML,
			},
		})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("bond with 2 ports", func() {
		bondYAML := `interfaces:
- name: bond99
  type: bond
  state: up
  ipv4:
    address:
    - ip: 192.0.2.0
      prefix-length: 24
    enabled: true
  link-aggregation:
    mode: balance-rr
    options:
      miimon: '140'
    port:
    - eth3
    - eth2`
		It("wrong interface names mapping", func() {
			err := staticNetworkGenerator.ValidateStaticConfigParams([]*models.HostStaticNetworkConfig{
				{
					MacInterfaceMap: []*models.MacInterfaceMapItems0{
						{
							LogicalNicName: "eth0",
							MacAddress:     "f8:75:a4:a4:00:fe",
						},
						{
							LogicalNicName: "eth1",
							MacAddress:     "f8:75:a4:a4:00:ff",
						},
					},
					NetworkYaml: bondYAML,
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("mac-interface mapping for interface"))
		})
		It("correct interface names mapping", func() {
			err := staticNetworkGenerator.ValidateStaticConfigParams([]*models.HostStaticNetworkConfig{
				{
					MacInterfaceMap: []*models.MacInterfaceMapItems0{
						{
							LogicalNicName: "eth2",
							MacAddress:     "f8:75:a4:a4:00:fe",
						},
						{
							LogicalNicName: "eth3",
							MacAddress:     "f8:75:a4:a4:00:ff",
						},
					},
					NetworkYaml: bondYAML,
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})
	})
	Context("vlan", func() {
		withoutUnderlyingInterface := `interfaces:
  - name: eth1.101
    type: vlan
    state: up
    vlan:
      base-iface: eth1
      id: 101`
		withUnderlyingInterface := `interfaces:
  - name: eth1
    type: ethernet
    state: up
  - name: eth1.101
    type: vlan
    state: up
    vlan:
      base-iface: eth1
      id: 101`
		It("vlan with underlying interface - no mapping", func() {
			err := staticNetworkGenerator.ValidateStaticConfigParams([]*models.HostStaticNetworkConfig{
				{
					MacInterfaceMap: nil,
					NetworkYaml:     withUnderlyingInterface,
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("mac-interface mapping for interface"))
		})
		It("vlan with underlying interface - with mapping", func() {
			err := staticNetworkGenerator.ValidateStaticConfigParams([]*models.HostStaticNetworkConfig{
				{
					MacInterfaceMap: models.MacInterfaceMap{
						{
							LogicalNicName: "eth1",
							MacAddress:     "f8:75:a4:a4:00:fe",
						},
					},
					NetworkYaml: withUnderlyingInterface,
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})
		It("vlan without underlying interface - with mapping", func() {
			err := staticNetworkGenerator.ValidateStaticConfigParams([]*models.HostStaticNetworkConfig{
				{
					MacInterfaceMap: models.MacInterfaceMap{
						{
							LogicalNicName: "eth1",
							MacAddress:     "f8:75:a4:a4:00:fe",
						},
					},
					NetworkYaml: withoutUnderlyingInterface,
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("no mapping for physical interfaces", func() {
		withPhysicalInterface := `interfaces:
  - name: eth0
    type: ethernet
    state: up
    ipv4:
      enabled: true
      dhcp: false
      address:
        - ip: 192.0.2.1
          prefix-length: 24
  - name: eno12399np0
    type: ethernet
    state: up
    ipv4:
      enabled: false
      dhcp: false`
		withNoMappedInterfaces := `interfaces:
  - name: eno12345
    type: ethernet
    state: up
    ipv4:
      enabled: false
      dhcp: false
  - name: eno12399np0
    type: ethernet
    state: up
    ipv4:
      enabled: false
      dhcp: false`
		withMacIdentifier := `interfaces:
  - name: eth0
    type: ethernet
    state: up
    identifier: mac-address
    mac-address: f8:75:a4:a4:00:fe
    ipv4:
      enabled: false
      dhcp: false`
		It("no mapping needed for physical interface", func() {
			err := staticNetworkGenerator.ValidateStaticConfigParams([]*models.HostStaticNetworkConfig{
				{
					MacInterfaceMap: []*models.MacInterfaceMapItems0{
						{
							LogicalNicName: "eth0",
							MacAddress:     "f8:75:a4:a4:00:fe",
						},
					},
					NetworkYaml: withPhysicalInterface,
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})
		It("at least one mapped interface is required", func() {
			err := staticNetworkGenerator.ValidateStaticConfigParams([]*models.HostStaticNetworkConfig{
				{
					MacInterfaceMap: []*models.MacInterfaceMapItems0{},
					NetworkYaml:     withNoMappedInterfaces,
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("at least one interface for host"))
		})
		It("mac-identifier field is allowed", func() {
			err := staticNetworkGenerator.ValidateStaticConfigParams([]*models.HostStaticNetworkConfig{
				{
					MacInterfaceMap: []*models.MacInterfaceMapItems0{
						{
							LogicalNicName: "eth0",
							MacAddress:     "f8:75:a4:a4:00:fe",
						},
					},
					NetworkYaml: withMacIdentifier,
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("StaticNetworkConfig", func() {
	var (
		staticNetworkGenerator = snc.New(logrus.New())
		multipleInterfacesYAML = `interfaces:
  - name: eth0
    type: ethernet
    state: up
    ipv4:
      enabled: true
      dhcp: false
      address:
        - ip: 192.0.2.1
          prefix-length: 24
  - name: eth1
    type: ethernet
    state: up
    ipv4:
      enabled: true
      dhcp: false
      address:
        - ip: 192.0.2.2
          prefix-length: 24
  - name: eth2
    type: ethernet
    state: up
    ipv4:
      enabled: true
      dhcp: false
      address:
        - ip: 192.0.2.3
          prefix-length: 24`
	)

	It("validate mac interface", func() {
		input := models.MacInterfaceMap{
			{LogicalNicName: "eth0", MacAddress: "macaddress0"},
			{LogicalNicName: "eth1", MacAddress: "macaddress1"},
			{LogicalNicName: "eth2", MacAddress: "macaddress2"},
		}
		staticNetworkConfig := []*models.HostStaticNetworkConfig{
			{
				MacInterfaceMap: input,
				NetworkYaml:     multipleInterfacesYAML,
			},
		}

		err := staticNetworkGenerator.ValidateStaticConfigParams(staticNetworkConfig)
		Expect(err).ToNot(HaveOccurred())

		input = models.MacInterfaceMap{
			{LogicalNicName: "eth0", MacAddress: "macaddress0"},
			{LogicalNicName: "eth1", MacAddress: "macaddress1"},
			{LogicalNicName: "eth0", MacAddress: "macaddress2"},
		}
		staticNetworkConfig = []*models.HostStaticNetworkConfig{
			{
				MacInterfaceMap: input,
				NetworkYaml:     multipleInterfacesYAML,
			},
		}
		err = staticNetworkGenerator.ValidateStaticConfigParams(staticNetworkConfig)
		Expect(err).To(HaveOccurred())

		input = models.MacInterfaceMap{
			{LogicalNicName: "eth0", MacAddress: "macaddress0"},
			{LogicalNicName: "eth1", MacAddress: "macaddress1"},
			{LogicalNicName: "eth2", MacAddress: "macaddress0"},
		}
		staticNetworkConfig = []*models.HostStaticNetworkConfig{
			{
				MacInterfaceMap: input,
				NetworkYaml:     multipleInterfacesYAML,
			},
		}
		err = staticNetworkGenerator.ValidateStaticConfigParams(staticNetworkConfig)
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

var _ = Describe("StaticNetworkConfig.GenerateStaticNetworkConfigDataYAML - generate nmpolicy", func() {
	var (
		staticNetworkGenerator = snc.New(logrus.New())

		hostsYAML, hostsYAMLAndIni = `[{ "network_yaml": "%s" }]`, `[{ "network_yaml": "%s", "mac_interface_map": %s }]`

		hostYAML = `interfaces:
- name: eth0
  type: ethernet
  state: up
  ipv4:
    enabled: true
    address:
      - ip: 192.0.2.1
        prefix-length: 24`

		macInterfaceMap = `[
        {
          "mac_address": "02:00:00:80:12:14",
          "logical_nic_name": "eth0"
        }
      ]`
	)
	escapedYamlContent, err := escapeYAMLForJSON(hostYAML)
	Expect(err).NotTo(HaveOccurred())

	It("Fail with an empty host YAML", func() {
		_, err := staticNetworkGenerator.GenerateStaticNetworkConfigDataYAML(`[{"network_yaml": ""}]`)
		Expect(err).To(HaveOccurred())
	})

	It("Fail with an invalid host YAML", func() {
		_, err := staticNetworkGenerator.GenerateStaticNetworkConfigDataYAML(`[{"network_yaml": "interfaces:\n    - foo: badConfig"}]`)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("InvalidArgument"))
	})

	It("Success - without ini file", func() {

		config, err := staticNetworkGenerator.GenerateStaticNetworkConfigDataYAML(fmt.Sprintf(hostsYAML, escapedYamlContent))
		fileContent := config[1].FileContents
		Expect(fileContent).To(ContainSubstring("capture"))
		Expect(err).NotTo(HaveOccurred())
	})
	It("Success - with ini file", func() {
		config, err := staticNetworkGenerator.GenerateStaticNetworkConfigDataYAML(fmt.Sprintf(hostsYAMLAndIni, escapedYamlContent, macInterfaceMap))
		fileContent := config[1].FileContents
		Expect(fileContent).To(ContainSubstring("name: \"{{ capture.iface0.interfaces.0.name }}\""))
		Expect(err).NotTo(HaveOccurred())
	})
})

// TODO: Implement this once a new Validator method is created that is appropriate for the nmstate service flow
var _ = Describe("StaticNetworkConfig.GenerateStaticNetworkConfigDataYAML - validate mac interface mapping", func() {

})

var _ = Describe("StaticNetworkConfig.GenerateStaticNetworkConfigArchive", func() {
	It("successfully produces an archive with one host data", func() {
		data := []snc.StaticNetworkConfigData{
			{
				FilePath:     "host1",
				FileContents: "static network config data of first host",
			},
		}
		archiveBytes, err := snc.GenerateStaticNetworkConfigArchive(data)
		Expect(err).ToNot(HaveOccurred())
		Expect(archiveBytes).ToNot(BeNil())
		checkArchiveString(archiveBytes.String(), data)
	})
	It("successfully produces an archive when file contents is empty", func() {
		data := []snc.StaticNetworkConfigData{
			{
				FilePath: "host1",
			},
		}
		archiveBytes, err := snc.GenerateStaticNetworkConfigArchive(data)
		Expect(err).ToNot(HaveOccurred())
		Expect(archiveBytes).ToNot(BeNil())
		checkArchiveString(archiveBytes.String(), data)
	})
	It("successfully produces an archive with multiple hosts' data", func() {
		data := []snc.StaticNetworkConfigData{
			{
				FilePath:     "host1",
				FileContents: "static network config data of first host",
			},
			{
				FilePath:     "host2",
				FileContents: "static network config data of second host",
			},
		}
		archiveBytes, err := snc.GenerateStaticNetworkConfigArchive(data)
		Expect(err).ToNot(HaveOccurred())
		Expect(archiveBytes).ToNot(BeNil())
		checkArchiveString(archiveBytes.String(), data)
	})

})

func checkArchiveString(archiveString string, allData []snc.StaticNetworkConfigData) {
	for _, data := range allData {
		Expect(archiveString).To(ContainSubstring("tar"))
		Expect(archiveString).To(ContainSubstring(filepath.Join("/etc/assisted/network", data.FilePath)))
		Expect(archiveString).To(ContainSubstring(data.FileContents))
	}
}

// escapeYAMLForJSON takes a YAML content string and escapes necessary characters to ensure it can be safely embedded within a JSON string.
func escapeYAMLForJSON(yamlContent string) (string, error) {
	// Use json.Marshal to escape the string
	escaped, err := json.Marshal(yamlContent)
	if err != nil {
		return "", err
	}

	// json.Marshal returns a byte slice with double quotes around the string,
	// so we need to trim the double quotes
	return string(escaped[1 : len(escaped)-1]), nil
}
