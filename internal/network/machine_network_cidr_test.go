package network

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
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

type FreeAddress struct {
	Network       string   `json:"network"`
	FreeAddresses []string `json:"free_addresses"`
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

	Context("VerifyVipsForClusterManagedLoadBalancer", func() {
		var (
			log logrus.FieldLogger
		)

		BeforeEach(func() {
			log = logrus.New()
		})

		Context("should fail verification", func() {
			It("when IP is not in the machine network", func() {
				machineNetworkCidr := "192.168.128.0/24"
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.127.2"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1", "192.168.127.2"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForClusterManagedLoadBalancer(
					hosts,
					machineNetworkCidr,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not belong to machine-network-cidr"))
			})

			It("when IP is the broadcast address of the machine network", func() {
				machineNetworkCidr := "192.168.127.0/24"
				apiVip := "192.168.127.255"
				ingressVIP := "192.168.127.2"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1", "192.168.127.2"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForClusterManagedLoadBalancer(
					hosts,
					machineNetworkCidr,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("is the broadcast address of machine-network-cidr"))
			})

			It("when API VIP is not in the free addresses list", func() {
				machineNetworkCidr := "192.168.127.0/24"
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.127.2"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.2"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForClusterManagedLoadBalancer(
					hosts,
					machineNetworkCidr,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("is already in use in cidr"))
			})

			It("when Ingress VIP is not in the free addresses list", func() {
				machineNetworkCidr := "192.168.127.0/24"
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.127.2"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForClusterManagedLoadBalancer(
					hosts,
					machineNetworkCidr,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("is already in use in cidr"))
			})

			It("when using common VIP", func() {
				machineNetworkCidr := "192.168.127.0/24"
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.127.1"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForClusterManagedLoadBalancer(
					hosts,
					machineNetworkCidr,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("appears both in apiVIPs and ingressVIPs"))
			})
		})

		Context("should pass verification", func() {
			It("with empty machine network", func() {
				machineNetworkCidr := ""
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.127.2"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1", "192.168.127.2"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForClusterManagedLoadBalancer(
					hosts,
					machineNetworkCidr,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			It("with no free addresses", func() {
				machineNetworkCidr := "192.168.127.0/24"
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.127.2"
				hosts := []*models.Host{{FreeAddresses: ""}}

				err := VerifyVipsForClusterManagedLoadBalancer(
					hosts,
					machineNetworkCidr,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			It("with standard configuration", func() {
				machineNetworkCidr := "192.168.127.0/24"
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.127.2"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1", "192.168.127.2"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForClusterManagedLoadBalancer(
					hosts,
					machineNetworkCidr,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Context("VerifyVipsForUserManangedLoadBalancer", func() {
		var log logrus.FieldLogger

		BeforeEach(func() {
			log = logrus.New()
		})

		Context("should fail verification", func() {
			It("with no machine networks", func() {
				machineNetworks := []*models.MachineNetwork{}
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.127.2"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1", "192.168.127.2"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForUserManangedLoadBalancer(
					hosts,
					machineNetworks,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no machine networks to verify VIP"))
			})

			It("with API VIP is not part of any machine network", func() {
				machineNetworks := []*models.MachineNetwork{
					{Cidr: "192.168.127.0/24"},
					{Cidr: "192.168.128.0/24"},
				}
				apiVip := "192.168.126.1"
				ingressVIP := "192.168.127.2"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1", "192.168.126.2"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForUserManangedLoadBalancer(
					hosts,
					machineNetworks,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not belong to machine-network-cidr"))
			})

			It("with Ingress VIP is not part of any machine network", func() {
				machineNetworks := []*models.MachineNetwork{
					{Cidr: "192.168.127.0/24"},
					{Cidr: "192.168.128.0/24"},
				}
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.126.2"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1", "192.168.126.2"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForUserManangedLoadBalancer(
					hosts,
					machineNetworks,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not belong to machine-network-cidr"))
			})

			It("with API VIP that is broadcast address in all machine networks", func() {
				machineNetworks := []*models.MachineNetwork{
					{Cidr: "192.168.127.0/24"},
				}
				apiVip := "192.168.127.255"
				ingressVIP := "192.168.127.1"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1", "192.168.127.2"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForUserManangedLoadBalancer(
					hosts,
					machineNetworks,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("is the broadcast address of machine-network-cidr"))
			})

			It("with Ingress VIP that is broadcast address in all machine networks", func() {
				machineNetworks := []*models.MachineNetwork{
					{Cidr: "192.168.127.0/24"},
				}
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.127.255"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1", "192.168.127.2"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForUserManangedLoadBalancer(
					hosts,
					machineNetworks,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("is the broadcast address of machine-network-cidr"))
			})

			It("when no machine networks satisfies the requirments", func() {
				machineNetworks := []*models.MachineNetwork{
					{Cidr: "192.168.127.0/24"},
					{Cidr: "192.168.128.0/24"},
				}
				apiVip := "192.168.129.1"
				ingressVIP := "192.168.128.1"
				hosts := []*models.Host{{}}

				err := VerifyVipsForUserManangedLoadBalancer(
					hosts,
					machineNetworks,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(
					"api-vip '192.168.129.1' failed verification: " +
						"none of the machine networks is satisfying the requirements, description: " +
						"machine network CIDR <192.168.127.0/24> verification failed: " +
						"api-vip <192.168.129.1> does not belong to machine-network-cidr <192.168.127.0/24>; " +
						"machine network CIDR <192.168.128.0/24> verification failed: " +
						"api-vip <192.168.129.1> does not belong to machine-network-cidr <192.168.128.0/24>",
				))
			})
		})

		Context("should pass verification", func() {
			It("when API VIP is not in the free addresses list", func() {
				machineNetworks := []*models.MachineNetwork{
					{Cidr: "192.168.127.0/24"},
				}
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.127.2"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.2"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForUserManangedLoadBalancer(
					hosts,
					machineNetworks,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			It("when Ingress VIP is not in the free addresses list", func() {
				machineNetworks := []*models.MachineNetwork{
					{Cidr: "192.168.127.0/24"},
				}
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.127.2"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForUserManangedLoadBalancer(
					hosts,
					machineNetworks,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			It("with common VIPs", func() {
				machineNetworks := []*models.MachineNetwork{
					{Cidr: "192.168.127.0/24"},
				}
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.127.1"

				bytes, err := json.Marshal([]FreeAddress{{
					Network:       "192.168.127.0/24",
					FreeAddresses: []string{"192.168.127.1"},
				}})
				Expect(err).ToNot(HaveOccurred())
				hosts := []*models.Host{{FreeAddresses: string(bytes)}}

				err = VerifyVipsForUserManangedLoadBalancer(
					hosts,
					machineNetworks,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			It("when at least one of the machine networks satisfies each VIP", func() {
				machineNetworks := []*models.MachineNetwork{
					{Cidr: "192.168.127.0/24"},
					{Cidr: "192.168.128.0/24"},
				}
				apiVip := "192.168.127.1"
				ingressVIP := "192.168.128.1"
				hosts := []*models.Host{{}}

				err := VerifyVipsForUserManangedLoadBalancer(
					hosts,
					machineNetworks,
					apiVip,
					ingressVIP,
					log,
				)
				Expect(err).ToNot(HaveOccurred())
			})

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

	Context("IsHostInAllMachineNetworksCidr", func() {
		var log logrus.FieldLogger

		BeforeEach(func() {
			log = logrus.New()
		})

		DescribeTable(
			"IsHostInAllMachineNetworksCidr",
			func(nics []*models.Interface, machineNetworks []*models.MachineNetwork, expectedResult bool) {
				cluster := createCluster("", "", createInventory(nics...))
				cluster.MachineNetworks = machineNetworks
				res := IsHostInAllMachineNetworksCidr(log, cluster, cluster.Hosts[0])
				Expect(res).To(Equal(expectedResult))
			},
			Entry("MachineNetworks is empty", []*models.Interface{createInterface("1.2.3.4/24")}, []*models.MachineNetwork{}, false),
			Entry("MachineNetworks is malformed", []*models.Interface{createInterface("1.2.3.4/24")}, []*models.MachineNetwork{{Cidr: "a.b.c.d"}}, false),
			Entry("Interfaces is empty", []*models.Interface{}, []*models.MachineNetwork{{Cidr: "a.b.c.d"}}, false),
			Entry("Interface IP is malformed", []*models.Interface{createInterface("a.b.c.d/24")}, []*models.MachineNetwork{{Cidr: "1.2.3.4/24"}}, false),
			Entry("Host belongs to all machine network CIDRs", []*models.Interface{createInterface("1.2.3.4/24"), addIPv6Addresses(createInterface(), "2001:db8::1/48")}, []*models.MachineNetwork{{Cidr: "1.2.3.0/24"}, {Cidr: "2001:db8::/48"}}, true),
			Entry("Host doesn't belong to all machine network CIDRs", []*models.Interface{createInterface("1.2.3.4/24")}, []*models.MachineNetwork{{Cidr: "1.2.3.0/24"}, {Cidr: "2001:db8::a1/120"}}, false),
		)
	})

	Context("IsInterfaceInPrimaryMachineNetCidr", func() {

		var log logrus.FieldLogger

		BeforeEach(func() {
			log = logrus.New()
		})

		DescribeTable(
			"IsInterfaceInPrimaryMachineNetCidr",
			func(nic *models.Interface, machineNetworks []*models.MachineNetwork, expectedResult bool) {
				cluster := createCluster("", "", createInventory(nic))
				cluster.MachineNetworks = machineNetworks
				res := IsInterfaceInPrimaryMachineNetCidr(log, cluster, nic)
				Expect(res).To(Equal(expectedResult))
			},
			Entry("MachineNetworks is empty", createInterface("1.2.3.4/24"), []*models.MachineNetwork{}, false),
			Entry("MachineNetworks is malformed", createInterface("1.2.3.4/24"), []*models.MachineNetwork{{Cidr: "a.b.c.d"}}, false),
			Entry("Interface IP is malformed", createInterface("a.b.c.d/24"), []*models.MachineNetwork{{Cidr: "1.2.3.4/24"}}, false),
			Entry("Interface belongs to a IPv4 machine network CIDR", addIPv6Addresses(createInterface("1.2.3.4/24"), "2001:db8::a1/48"), []*models.MachineNetwork{{Cidr: "1.2.3.4/24"}}, true),
			Entry("Interface belongs to a IPv6 machine network CIDR", addIPv6Addresses(createInterface("1.2.3.4/24"), "2001:db8::a1/48"), []*models.MachineNetwork{{Cidr: "2001:db8::/48"}}, true),
			Entry("Interface doesn't belong to any machine network CIDRs", createInterface("1.2.3.4/24", "2001:db8::a1/48"), []*models.MachineNetwork{{Cidr: "5.6.7.8/24"}, {Cidr: "2001:db9::/48"}}, false),
		)
	})

	Context("SetBootStrapHostIPRelatedMachineNetworkFirst", func() {
		var (
			log    *logrus.Logger
			logger logrus.FieldLogger
		)

		BeforeEach(func() {
			log = logrus.New()
			logger = log
		})

		Context("Error Scenarios", func() {
			It("should return error when cluster is nil", func() {
				machineNets, err := SetBootStrapHostIPRelatedMachineNetworkFirst(nil, logger)
				Expect(machineNets).To(BeNil())
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("given cluster is nil"))
			})

			It("should return error when cluster.ID is nil", func() {
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						MachineNetworks: []*models.MachineNetwork{createMachineNetwork("1.2.3.0/24")},
					},
				}

				machineNets, err := SetBootStrapHostIPRelatedMachineNetworkFirst(cluster, logger)
				Expect(machineNets).To(Equal(cluster.MachineNetworks))
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("given cluster ID is nil"))
			})

			It("should return error when no bootstrap host is found", func() {
				clusterID := strToUUID("01234567-89ab-cdef-0123-456789abcdef")
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID:              clusterID,
						MachineNetworks: []*models.MachineNetwork{createMachineNetwork("1.2.3.0/24")},
						Hosts:           []*models.Host{createHost(false, []string{"1.2.3.10/24"}, nil)},
					},
				}
				machineNets, err := SetBootStrapHostIPRelatedMachineNetworkFirst(cluster, logger)
				Expect(machineNets).To(Equal(cluster.MachineNetworks))
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("cluster %s has no bootstrap host", clusterID.String())))
			})

			It("should return error if machine network CIDR is invalid (parse error)", func() {
				clusterID := strToUUID("22234567-89ab-cdef-0123-456789abcdef")
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID: clusterID,
						MachineNetworks: []*models.MachineNetwork{
							createMachineNetwork("not-a-valid-cidr"),
							createMachineNetwork("1.2.3.0/24"),
						},
						Hosts: []*models.Host{
							createHost(true, []string{"1.2.3.10/24"}, nil),
						},
					},
				}
				machineNets, err := SetBootStrapHostIPRelatedMachineNetworkFirst(cluster, logger)

				Expect(machineNets).To(Equal(cluster.MachineNetworks))
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to parse machine cidr: not-a-valid-cidr"))
			})

			It("should return error if no machine network matches the bootstrap host IP", func() {
				clusterID := strToUUID("33334567-89ab-cdef-0123-456789abcdef")
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID: clusterID,
						MachineNetworks: []*models.MachineNetwork{
							createMachineNetwork("10.10.0.0/24"),
							createMachineNetwork("192.168.100.0/24"),
						},
						Hosts: []*models.Host{
							createHost(true, []string{"172.16.0.10/24"}, nil),
						},
					},
				}
				machineNets, err := SetBootStrapHostIPRelatedMachineNetworkFirst(cluster, logger)
				Expect(machineNets).To(Equal(cluster.MachineNetworks))
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("none of the machine network cidrs contain any of the bootstrap host IPs"))
			})
		})

		Context("Success Scenarios", func() {
			It("should do nothing if the matched machine network is already the first in the list", func() {
				clusterID := strToUUID("44434567-89ab-cdef-0123-456789abcdef")
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID: clusterID,
						MachineNetworks: []*models.MachineNetwork{
							createMachineNetwork("1.2.3.0/24"),
							createMachineNetwork("192.168.100.0/24"),
						},
						Hosts: []*models.Host{
							createHost(true, []string{"1.2.3.10/24"}, nil),
						},
					},
				}

				newNets, err := SetBootStrapHostIPRelatedMachineNetworkFirst(cluster, logger)
				Expect(err).NotTo(HaveOccurred())
				Expect(newNets).To(Equal(cluster.MachineNetworks))
			})

			It("should move the matched machine network to first position if it is last", func() {
				clusterID := strToUUID("55534567-89ab-cdef-0123-456789abcdef")
				net1 := createMachineNetwork("1.2.3.0/24")
				net2 := createMachineNetwork("192.168.100.0/24")
				net3 := createMachineNetwork("10.10.10.0/24")
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID:              clusterID,
						MachineNetworks: []*models.MachineNetwork{net1, net2, net3},
						Hosts: []*models.Host{
							createHost(true, []string{"10.10.10.55/24"}, nil),
						},
					},
				}

				newNets, err := SetBootStrapHostIPRelatedMachineNetworkFirst(cluster, logger)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(newNets)).To(Equal(3))
				Expect(newNets[0]).To(Equal(net3))
				Expect(newNets[1]).To(Equal(net1))
				Expect(newNets[2]).To(Equal(net2))
			})

			It("should move the matched machine network to first position if it is in the middle", func() {
				clusterID := strToUUID("66634567-89ab-cdef-0123-456789abcdef")
				net1 := createMachineNetwork("1.2.3.0/24")
				net2 := createMachineNetwork("192.168.100.0/24")
				net3 := createMachineNetwork("10.10.10.0/24")
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID:              clusterID,
						MachineNetworks: []*models.MachineNetwork{net1, net2, net3},
						Hosts: []*models.Host{
							createHost(true, []string{"192.168.100.20/24"}, nil),
						},
					},
				}

				newNets, err := SetBootStrapHostIPRelatedMachineNetworkFirst(cluster, logger)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(newNets)).To(Equal(3))

				Expect(newNets[0]).To(Equal(net2))
				Expect(newNets[1]).To(Equal(net1))
				Expect(newNets[2]).To(Equal(net3))
			})

			It("should handle nil machine network entries gracefully (skip them)", func() {
				clusterID := strToUUID("77734567-89ab-cdef-0123-456789abcdef")
				net1 := createMachineNetwork("1.2.3.0/24")

				var netNil *models.MachineNetwork
				net3 := createMachineNetwork("10.10.10.0/24")
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID:              clusterID,
						MachineNetworks: []*models.MachineNetwork{net1, netNil, net3},
						Hosts: []*models.Host{
							createHost(true, []string{"10.10.10.55/24"}, nil),
						},
					},
				}

				newNets, err := SetBootStrapHostIPRelatedMachineNetworkFirst(cluster, logger)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(newNets)).To(Equal(3))

				Expect(newNets[0]).To(Equal(net3))
				Expect(newNets[1]).To(Equal(net1))
				Expect(newNets[2]).To(BeNil())
			})

			Context("Edge Cases", func() {
				It("multiple networks match the bootstrap host's IP (it will pick the last match)", func() {
					clusterID := strToUUID("88834567-89ab-cdef-0123-456789abcdef")
					net1 := createMachineNetwork("1.2.0.0/16")
					net2 := createMachineNetwork("1.2.3.0/24")
					net3 := createMachineNetwork("192.168.0.0/24")
					cluster := &common.Cluster{
						Cluster: models.Cluster{
							ID:              clusterID,
							MachineNetworks: []*models.MachineNetwork{net1, net2, net3},
							Hosts: []*models.Host{
								createHost(true, []string{"1.2.3.4/24"}, nil),
							},
						},
					}

					newNets, err := SetBootStrapHostIPRelatedMachineNetworkFirst(cluster, logger)
					Expect(err).NotTo(HaveOccurred())
					Expect(len(newNets)).To(Equal(3))
					Expect(newNets[0]).To(Equal(net2))
					Expect(newNets[1]).To(Equal(net1))
					Expect(newNets[2]).To(Equal(net3))
				})
			})
		})
	})
})

func strToUUID(uuidStr string) *strfmt.UUID {
	uid := strfmt.UUID(uuidStr)
	return &uid
}

func createMachineNetwork(cidr string) *models.MachineNetwork {
	return &models.MachineNetwork{Cidr: models.Subnet(cidr)}
}

func createHost(bootstrap bool, ipv4Addresses, ipv6Addresses []string) *models.Host {
	inv := models.Inventory{
		Interfaces: []*models.Interface{
			{
				Name:          "test-nic",
				IPV4Addresses: ipv4Addresses,
				IPV6Addresses: ipv6Addresses,
			},
		},
	}
	invBytes, _ := json.Marshal(&inv)
	return &models.Host{
		Bootstrap: bootstrap,
		Inventory: string(invBytes),
	}
}

func TestMachineNetworkCidr(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Machine network cider Suite")
}
