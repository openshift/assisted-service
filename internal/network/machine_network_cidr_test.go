package network

import (
	"encoding/json"
	"testing"

	"github.com/go-openapi/swag"

	"github.com/openshift/assisted-service/internal/common"

	"github.com/sirupsen/logrus"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("inventory", func() {

	createInterface := func(ipv4Addresses ...string) *models.Interface {
		return &models.Interface{
			IPV4Addresses: append([]string{}, ipv4Addresses...),
		}
	}

	createInventory := func(interfaces ...*models.Interface) string {
		inventory := models.Inventory{Interfaces: interfaces}
		ret, _ := json.Marshal(&inventory)
		return string(ret)
	}

	createHosts := func(inventories ...string) []*models.Host {
		ret := make([]*models.Host, 0)
		for _, i := range inventories {
			ret = append(ret, &models.Host{Inventory: i})
		}
		return ret
	}

	createDisabledHosts := func(inventories ...string) []*models.Host {
		ret := make([]*models.Host, 0)
		for _, i := range inventories {
			ret = append(ret, &models.Host{Inventory: i,
				Status: swag.String(models.HostStatusDisabled)})
		}
		return ret
	}

	createCluster := func(apiVip string, machineCidr string, inventories ...string) *common.Cluster {
		return &common.Cluster{Cluster: models.Cluster{
			APIVip:             apiVip,
			MachineNetworkCidr: machineCidr,
			Hosts:              createHosts(inventories...),
		}}
	}
	createDisabledCluster := func(apiVip string, machineCidr string, inventories ...string) *common.Cluster {
		return &common.Cluster{Cluster: models.Cluster{
			APIVip:             apiVip,
			MachineNetworkCidr: machineCidr,
			Hosts:              createDisabledHosts(inventories...),
		}}
	}
	Context("CalculateMachineNetworkCIDR", func() {
		It("happpy flow", func() {
			cluster := createCluster("1.2.5.6", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(cluster.APIVip, cluster.IngressVip, cluster.Hosts)
			Expect(err).To(Not(HaveOccurred()))
			Expect(cidr).To(Equal("1.2.4.0/23"))
		})

		It("Disabled", func() {
			cluster := createDisabledCluster("1.2.5.6", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			_, err := CalculateMachineNetworkCIDR(cluster.APIVip, cluster.IngressVip, cluster.Hosts)
			Expect(err).To(HaveOccurred())
		})

		It("Illegal VIP", func() {
			cluster := createCluster("1.2.5.257", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(cluster.APIVip, cluster.IngressVip, cluster.Hosts)
			Expect(err).To(HaveOccurred())
			Expect(cidr).To(Equal(""))
		})

		It("No Match", func() {
			cluster := createCluster("1.2.5.200", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.6.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(cluster.APIVip, cluster.IngressVip, cluster.Hosts)
			Expect(err).To(HaveOccurred())
			Expect(cidr).To(Equal(""))
		})
		It("Bad inventory", func() {
			cluster := createCluster("1.2.5.6", "",
				"Bad inventory",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(cluster.APIVip, cluster.IngressVip, cluster.Hosts)
			Expect(err).To(Not(HaveOccurred()))
			Expect(cidr).To(Equal("1.2.4.0/23"))
		})
	})
	Context("GetMachineCIDRHosts", func() {
		It("No Machine CIDR", func() {
			cluster := createCluster("1.2.5.6", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			_, err := GetMachineCIDRHosts(logrus.New(), cluster)
			Expect(err).To(HaveOccurred())
		})
		It("No matching Machine CIDR", func() {
			cluster := createCluster("1.2.5.6", "1.1.0.0/16",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			hosts, err := GetMachineCIDRHosts(logrus.New(), cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(hosts).To(BeEmpty())
		})
		It("Some matched", func() {
			cluster := createCluster("1.2.5.6", "1.2.4.0/23",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")),
				createInventory(createInterface("1.2.4.79/23")))
			hosts, err := GetMachineCIDRHosts(logrus.New(), cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(hosts).To(Equal([]*models.Host{
				cluster.Hosts[0],
				cluster.Hosts[2],
			}))

		})
	})
	Context("VerifyVips", func() {
		var log logrus.FieldLogger

		BeforeEach(func() {
			log = logrus.New()
		})
		It("Same vips", func() {
			cluster := createCluster("1.2.5.6", "1.2.4.0/23",
				createInventory(createInterface("1.2.5.7/23")))
			cluster.Hosts = []*models.Host{
				{
					FreeAddresses: "[{\"network\":\"1.2.4.0/23\",\"free_addresses\":[\"1.2.5.6\",\"1.2.5.8\"]}]",
				},
			}
			cluster.IngressVip = cluster.APIVip
			err := VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, false, log)
			Expect(err).To(HaveOccurred())
			err = VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, true, log)
			Expect(err).To(HaveOccurred())
		})
		It("Different vips", func() {
			cluster := createCluster("1.2.5.6", "1.2.4.0/23",
				createInventory(createInterface("1.2.5.7/23")))
			cluster.IngressVip = "1.2.5.8"
			cluster.Hosts = []*models.Host{
				{
					FreeAddresses: "[{\"network\":\"1.2.4.0/23\",\"free_addresses\":[\"1.2.5.6\",\"1.2.5.8\"]}]",
				},
			}
			err := VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, false, log)
			Expect(err).ToNot(HaveOccurred())
			err = VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, true, log)
			Expect(err).ToNot(HaveOccurred())
		})
		It("Not free", func() {
			cluster := createCluster("1.2.5.6", "1.2.4.0/23",
				createInventory(createInterface("1.2.5.7/23")))
			cluster.IngressVip = "1.2.5.8"
			cluster.Hosts = []*models.Host{
				{
					FreeAddresses: "[{\"network\":\"1.2.4.0/23\",\"free_addresses\":[\"1.2.5.9\"]}]",
				},
			}
			err := VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, false, log)
			Expect(err).To(HaveOccurred())
			err = VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, true, log)
			Expect(err).To(HaveOccurred())
		})
		It("Disabled", func() {
			cluster := createCluster("1.2.5.6", "1.2.4.0/23",
				createInventory(createInterface("1.2.5.7/23")))
			cluster.IngressVip = "1.2.5.8"
			cluster.Hosts = []*models.Host{
				{
					FreeAddresses: "[{\"network\":\"1.2.4.0/23\",\"free_addresses\":[\"1.2.5.9\"]}]",
					Status:        swag.String(models.HostStatusDisabled),
				},
			}
			err := VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, false, log)
			Expect(err).ToNot(HaveOccurred())
			err = VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, true, log)
			Expect(err).ToNot(HaveOccurred())
		})
		It("Empty", func() {
			cluster := createCluster("1.2.5.6", "1.2.4.0/23",
				createInventory(createInterface("1.2.5.7/23")))
			cluster.IngressVip = "1.2.5.8"
			cluster.Hosts = []*models.Host{
				{
					FreeAddresses: "",
				},
			}
			err := VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, false, log)
			Expect(err).ToNot(HaveOccurred())
			err = VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, true, log)
			Expect(err).ToNot(HaveOccurred())
		})
		It("Free", func() {
			cluster := createCluster("1.2.5.6", "1.2.4.0/23",
				createInventory(createInterface("1.2.5.7/23")))
			cluster.IngressVip = "1.2.5.8"
			cluster.Hosts = []*models.Host{
				{
					FreeAddresses: "[{\"network\":\"1.2.4.0/23\",\"free_addresses\":[\"1.2.5.6\",\"1.2.5.8\",\"1.2.5.9\"]}]",
				},
			}
			err := VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, false, log)
			Expect(err).ToNot(HaveOccurred())
			err = VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, true, log)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func TestMachineNetworkCidr(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Machine network cider Suite")
}
