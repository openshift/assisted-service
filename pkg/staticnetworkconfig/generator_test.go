package staticnetworkconfig

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"testing"

	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
)

func TestStaticNetworkConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "StaticNetworkConfig Suite")
}

var _ = Describe("StaticNetworkConfig", func() {
	ctrl := gomock.NewController(GinkgoT())
	mockExecuter := executer.NewMockExecuter(ctrl)
	var (
		staticNetworkGenerator = StaticNetworkConfigGenerator{log: logrus.New(), sem: semaphore.NewWeighted(1), executer: mockExecuter}
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

	It("generate static network config data", func() {
		ctx := context.TODO()
		input := ""
		staticNetworkConfigData, err := staticNetworkGenerator.GenerateStaticNetworkConfigData(ctx, input)
		Expect(err).ToNot(HaveOccurred())
		Expect(staticNetworkConfigData).To(BeEmpty())

		input = "invalid_yaml"
		staticNetworkConfigData, err = staticNetworkGenerator.GenerateStaticNetworkConfigData(ctx, input)
		Expect(err).To(HaveOccurred())
		Expect(staticNetworkConfigData).To(BeEmpty())

	})

	It("static config params validation error", func() {
		ctx := context.TODO()
		staticNetworkConfig := []*models.HostStaticNetworkConfig{}

		file, err := ioutil.TempFile("/tmp", "host-config")
		Expect(err).ToNot(HaveOccurred())
		mockExecuter.EXPECT().TempFile("", "host-config").Return(file, nil).Times(1)
		mockExecuter.EXPECT().ExecuteWithContext(ctx, "nmstatectl", "gc", file.Name()).Return("", "", 0).Times(1)

		err = staticNetworkGenerator.ValidateStaticConfigParams(ctx, staticNetworkConfig)
		Expect(err).ToNot(HaveOccurred())

		staticNetworkConfig = []*models.HostStaticNetworkConfig{
			common.FormatStaticConfigHostYAML("nic10", "02000048ba38", "192.168.126.30", "192.168.141.30", "192.168.126.1",
				models.MacInterfaceMap{
					&models.MacInterfaceMapItems0{MacAddress: "mac10", LogicalNicName: "nic10"},
				}),
		}
		err = staticNetworkGenerator.ValidateStaticConfigParams(ctx, staticNetworkConfig)
		Expect(err).To(HaveOccurred())
	})

	It("static config params validation", func() {
		ctx := context.TODO()
		staticNetworkConfig := []*models.HostStaticNetworkConfig{
			common.FormatStaticConfigHostYAML("nic10", "02000048ba38", "192.168.126.30", "192.168.141.30", "192.168.126.1",
				models.MacInterfaceMap{
					&models.MacInterfaceMapItems0{MacAddress: "mac10", LogicalNicName: "nic10"},
				}),
		}

		nmStateCtlOutput := `NetworkManager:
- - foo.nmconnection
  - '[connection]
    id=foo'`

		file, err := ioutil.TempFile("/tmp", "host-config")
		Expect(err).ToNot(HaveOccurred())
		mockExecuter.EXPECT().TempFile("", "host-config").Return(file, nil).Times(1)
		mockExecuter.EXPECT().ExecuteWithContext(ctx, "nmstatectl", "gc", file.Name()).Return(nmStateCtlOutput, "", 0).Times(1)

		err = staticNetworkGenerator.ValidateStaticConfigParams(ctx, staticNetworkConfig)
		Expect(err).ToNot(HaveOccurred())

		nmStateCtlOutput = `NetworkManager:
- - foo.nmconnection`

		file, err = ioutil.TempFile("/tmp", "host-config")
		Expect(err).ToNot(HaveOccurred())
		mockExecuter.EXPECT().TempFile("", "host-config").Return(file, nil).Times(1)
		mockExecuter.EXPECT().ExecuteWithContext(ctx, "nmstatectl", "gc", file.Name()).Return(nmStateCtlOutput, "", 0).Times(1)

		err = staticNetworkGenerator.ValidateStaticConfigParams(ctx, staticNetworkConfig)
		Expect(err).To(HaveOccurred())

		nmStateCtlOutput = `NetworkManager:
- - foo.nmconnection
	- [some-random-key]`

		file, err = ioutil.TempFile("/tmp", "host-config")
		Expect(err).ToNot(HaveOccurred())
		mockExecuter.EXPECT().TempFile("", "host-config").Return(file, nil).Times(1)
		mockExecuter.EXPECT().ExecuteWithContext(ctx, "nmstatectl", "gc", file.Name()).Return(nmStateCtlOutput, "", 0).Times(1)

		err = staticNetworkGenerator.ValidateStaticConfigParams(ctx, staticNetworkConfig)
		Expect(err).To(HaveOccurred())
	})
})
