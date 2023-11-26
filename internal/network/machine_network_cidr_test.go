package network

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

func createInventory(interfaces ...*models.Interface) string {
	inventory := models.Inventory{Interfaces: interfaces}
	ret, _ := json.Marshal(&inventory)
	return string(ret)
}

func createInterface(ipv4Addresses ...string) *models.Interface {
	return &models.Interface{
		IPV4Addresses: append([]string{}, ipv4Addresses...),
		Name:          "test",
	}
}

func addIPv6Addresses(nic *models.Interface, ipv6Addresses ...string) *models.Interface {
	nic.IPV6Addresses = append([]string{}, ipv6Addresses...)
	return nic
}

func createHosts(inventories ...string) []*models.Host {
	ret := make([]*models.Host, 0)
	for _, i := range inventories {
		ret = append(ret, &models.Host{Inventory: i})
	}
	return ret
}

func createCluster(apiVip string, machineCidr string, inventories ...string) *common.Cluster {
	return &common.Cluster{Cluster: models.Cluster{
		APIVips:         []*models.APIVip{{IP: models.IP(apiVip)}},
		MachineNetworks: CreateMachineNetworksArray(machineCidr),
		Hosts:           createHosts(inventories...),
	}}
}

var _ = Describe("inventory", func() {

	Context("CalculateMachineNetworkCIDR", func() {
		It("happpy flow", func() {
			cluster := createCluster("1.2.5.6", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0), cluster.Hosts, true)
			Expect(err).To(Not(HaveOccurred()))
			Expect(cidr).To(Equal("1.2.4.0/23"))
		})

		It("happy flow IPv6", func() {
			cluster := createCluster("1001:db8::64", "",
				createInventory(addIPv6Addresses(createInterface(), "1001:db8::1/120")),
				createInventory(addIPv6Addresses(createInterface(), "1001:db8::2/120")),
				createInventory(addIPv6Addresses(createInterface(), "1001:db8::3/120")))
			cidr, err := CalculateMachineNetworkCIDR(GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0), cluster.Hosts, true)
			Expect(err).To(Not(HaveOccurred()))
			Expect(cidr).To(Equal("1001:db8::/120"))
		})

		It("Illegal VIP", func() {
			cluster := createCluster("1.2.5.257", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0), cluster.Hosts, true)
			Expect(err).To(HaveOccurred())
			Expect(cidr).To(Equal(""))
		})

		It("No Match", func() {
			cluster := createCluster("1.2.5.200", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.6.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0), cluster.Hosts, true)
			Expect(err).To(HaveOccurred())
			Expect(cidr).To(Equal(""))
		})
		It("Bad inventory", func() {
			cluster := createCluster("1.2.5.6", "",
				"Bad inventory",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0), cluster.Hosts, true)
			Expect(err).To(Not(HaveOccurred()))
			Expect(cidr).To(Equal("1.2.4.0/23"))
		})
		It("No Match - no match required", func() {
			cluster := createCluster("1.2.5.200", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.6.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0), cluster.Hosts, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(cidr).To(Equal(""))
		})
	})
	Context("GetMachineCIDRHosts", func() {
		It("No Machine CIDR", func() {
			cluster := createCluster("1.2.5.6", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			_, err := GetPrimaryMachineCIDRHosts(logrus.New(), cluster)
			Expect(err).To(HaveOccurred())
		})
		It("No matching Machine CIDR", func() {
			cluster := createCluster("1.2.5.6", "1.1.0.0/16",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			hosts, err := GetPrimaryMachineCIDRHosts(logrus.New(), cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(hosts).To(BeEmpty())
		})
		It("Some matched", func() {
			cluster := createCluster("1.2.5.6", "1.2.4.0/23",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")),
				createInventory(createInterface("1.2.4.79/23")))
			hosts, err := GetPrimaryMachineCIDRHosts(logrus.New(), cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(hosts).To(Equal([]*models.Host{
				cluster.Hosts[0],
				cluster.Hosts[2],
			}))

		})
	})
	Context("VerifyVips", func() {
		var (
			log                logrus.FieldLogger
			primaryMachineCidr = "1.2.4.0/23"
		)

		BeforeEach(func() {
			log = logrus.New()
		})
		It("Same vips", func() {
			cluster := createCluster("1.2.5.6", primaryMachineCidr,
				createInventory(createInterface("1.2.5.7/23")))
			cluster.Hosts = []*models.Host{
				{
					FreeAddresses: "[{\"network\":\"1.2.4.0/23\",\"free_addresses\":[\"1.2.5.6\",\"1.2.5.8\"]}]",
				},
			}
			cluster.IngressVips = []*models.IngressVip{{IP: models.IP(GetApiVipById(cluster, 0))}}
			err := VerifyVips(cluster.Hosts, primaryMachineCidr, GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0), log)
			Expect(err).To(HaveOccurred())
		})
		It("Different vips", func() {
			cluster := createCluster("1.2.5.6", primaryMachineCidr,
				createInventory(createInterface("1.2.5.7/23")))
			cluster.IngressVips = []*models.IngressVip{{IP: "1.2.5.8"}}
			cluster.Hosts = []*models.Host{
				{
					FreeAddresses: "[{\"network\":\"1.2.4.0/23\",\"free_addresses\":[\"1.2.5.6\",\"1.2.5.8\"]}]",
				},
			}
			err := VerifyVips(cluster.Hosts, primaryMachineCidr, GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0), log)
			Expect(err).ToNot(HaveOccurred())
		})
		It("Not free", func() {
			cluster := createCluster("1.2.5.6", primaryMachineCidr,
				createInventory(createInterface("1.2.5.7/23")))
			cluster.IngressVips = []*models.IngressVip{{IP: "1.2.5.8"}}
			cluster.Hosts = []*models.Host{
				{
					FreeAddresses: "[{\"network\":\"1.2.4.0/23\",\"free_addresses\":[\"1.2.5.9\"]}]",
				},
			}
			err := VerifyVips(cluster.Hosts, primaryMachineCidr, GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0), log)
			Expect(err).To(HaveOccurred())
		})
		It("Empty", func() {
			cluster := createCluster("1.2.5.6", primaryMachineCidr,
				createInventory(createInterface("1.2.5.7/23")))
			cluster.IngressVips = []*models.IngressVip{{IP: "1.2.5.8"}}
			cluster.Hosts = []*models.Host{
				{
					FreeAddresses: "",
				},
			}
			err := VerifyVips(cluster.Hosts, primaryMachineCidr, GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0), log)
			Expect(err).ToNot(HaveOccurred())
		})
		It("Free", func() {
			cluster := createCluster("1.2.5.6", primaryMachineCidr,
				createInventory(createInterface("1.2.5.7/23")))
			cluster.IngressVips = []*models.IngressVip{{IP: "1.2.5.8"}}
			cluster.Hosts = []*models.Host{
				{
					FreeAddresses: "[{\"network\":\"1.2.4.0/23\",\"free_addresses\":[\"1.2.5.6\",\"1.2.5.8\",\"1.2.5.9\"]}]",
				},
			}
			err := VerifyVips(cluster.Hosts, primaryMachineCidr, GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0), log)
			Expect(err).ToNot(HaveOccurred())
		})
		It("machine cidr is too small", func() {
			cluster := createCluster("1.2.5.2", "1.2.5.0/29", createInventory(
				createInterface("1.2.5.2/29"),
				createInterface("1.2.5.3/29"),
				createInterface("1.2.5.4/29"),
				createInterface("1.2.5.5/29"),
				createInterface("1.2.5.6/29")))
			h := &models.Host{
				FreeAddresses: "[{\"network\":\"1.2.5.0/29\",\"free_addresses\":[\"1.2.5.7\"]}]",
			}
			cluster.Hosts = []*models.Host{h, h, h, h, h}
			cluster.APIVips = []*models.APIVip{{IP: "1.2.5.2"}}
			err := VerifyVips(cluster.Hosts, "1.2.5.0/29", GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0), log)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("The machine network range is too small for the cluster"))
		})
	})

	Context("GetInventoryNetworks", func() {

		var log logrus.FieldLogger

		BeforeEach(func() {
			log = logrus.New()
		})

		It("No hosts", func() {
			nets := GetInventoryNetworks(createHosts(), log)
			Expect(nets).To(BeEmpty())
		})

		It("Empty inventory", func() {
			nets := GetInventoryNetworks(createHosts(
				"",
				createInventory(createInterface("2.2.3.10/24"))), log)
			Expect(nets).To(HaveLen(1))
			Expect(nets[0]).To(Equal("2.2.3.0/24"))
		})

		It("Corrupted inventory", func() {
			nets := GetInventoryNetworks(createHosts(
				"{\"interfaces:}",
				createInventory(createInterface("1.2.3.5/28"))), log)
			Expect(nets).To(HaveLen(1))
			Expect(nets[0]).To(Equal("1.2.3.0/28"))
		})

		It("No interfaces", func() {
			nets := GetInventoryNetworks(createHosts(
				createInventory(createInterface("10.2.3.20/24")),
				createInventory()), log)
			Expect(nets).To(HaveLen(1))
			Expect(nets[0]).To(Equal("10.2.3.0/24"))
		})

		It("IPv4 only", func() {
			nets := GetInventoryNetworks(createHosts(
				createInventory(createInterface("10.2.3.20/24", "1.2.3.4/28")),
				createInventory(createInterface("198.2.3.10/28"))), log)
			Expect(nets).To(HaveLen(3))
			Expect(nets).To(ContainElements("10.2.3.0/24", "1.2.3.0/28", "198.2.3.0/28"))
		})

		It("IPv6 only", func() {
			nets := GetInventoryNetworks(createHosts(
				createInventory(addIPv6Addresses(createInterface(), "2001:db8::a1/120")),
				createInventory(addIPv6Addresses(createInterface(), "fe80:5054::4/120", "2002:db8::a1/120"))), log)
			Expect(nets).To(HaveLen(3))
			Expect(nets).To(ContainElements("2001:db8::/120", "fe80:5054::/120", "2002:db8::/120"))
		})

		It("Dual stack", func() {
			nets := GetInventoryNetworks(createHosts(
				createInventory(addIPv6Addresses(createInterface("1.2.3.4/28"), "2001:db8::a1/120")),
				createInventory(addIPv6Addresses(createInterface("10.2.3.20/24"), "fe80:5054::4/120"))), log)
			Expect(nets).To(HaveLen(4))
			Expect(nets).To(ContainElements("2001:db8::/120", "fe80:5054::/120", "10.2.3.0/24", "1.2.3.0/28"))
		})

		It("Invalid CIDR", func() {
			nets := GetInventoryNetworks(createHosts(
				createInventory(addIPv6Addresses(createInterface("1.2.260.4/28"), "2001:db8::a1/120")),
				createInventory(addIPv6Addresses(createInterface("10.2.3.20/24"), "fe80:5054::4"))), log)
			Expect(nets).To(HaveLen(2))
			Expect(nets).To(ContainElements("2001:db8::/120", "10.2.3.0/24"))
		})

		It("Same CIDR", func() {
			nets := GetInventoryNetworks(createHosts(
				createInventory(addIPv6Addresses(createInterface("1.2.3.4/28"), "2001:db8::a1/120")),
				createInventory(addIPv6Addresses(createInterface("1.2.3.10/28"), "2001:db8::5/120"))), log)
			Expect(nets).To(HaveLen(2))
			Expect(nets).To(ContainElements("2001:db8::/120", "1.2.3.0/28"))
		})
	})

	Context("GetMachineCidrForUserManagedNetwork", func() {

		var log logrus.FieldLogger

		BeforeEach(func() {
			log = logrus.New()
		})

		It("No bootstrap host", func() {
			cluster := createCluster("", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			machineCidr := GetPrimaryMachineCidrForUserManagedNetwork(cluster, log)
			Expect(machineCidr).To(BeEmpty())
		})

		It("No machine cidr was set - cidr from bootstrap must be set", func() {
			cluster := createCluster("", "",
				createInventory(addIPv6Addresses(createInterface("1.2.3.4/28"), "2001:db8::a1/120")),
				createInventory(addIPv6Addresses(createInterface("10.2.3.20/24"), "fe80:5054::4/120")))
			cluster.Hosts[0].Bootstrap = true

			machineCidr := GetPrimaryMachineCidrForUserManagedNetwork(cluster, log)
			Expect(true).To(Equal(machineCidr == "1.2.3.0/28" || machineCidr == "2001:db8::/120"))
		})

		It("Machine cidr exists", func() {
			cluster := createCluster("", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "1.2.5.0/23"}}
			machineCidr := GetPrimaryMachineCidrForUserManagedNetwork(cluster, log)
			Expect(machineCidr).To(Equal(GetMachineCidrById(cluster, 0)))
		})

	})
})

func TestMachineNetworkCidr(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Machine network cider Suite")
}
