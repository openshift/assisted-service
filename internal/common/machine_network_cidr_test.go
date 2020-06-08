package common

import (
	"encoding/json"

	"github.com/filanov/bm-inventory/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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

	createCluster := func(apiVip string, machineCidr string, inventories ...string) *models.Cluster {
		return &models.Cluster{
			APIVip:             apiVip,
			MachineNetworkCidr: machineCidr,
			Hosts:              createHosts(inventories...),
		}
	}
	Context("CalculateMachineNetworkCIDR", func() {
		It("happpy flow", func() {
			cluster := createCluster("1.2.5.6", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(cidr).To(Equal("1.2.4.0/23"))
		})

		It("Illegal VIP", func() {
			cluster := createCluster("1.2.5.257", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(cluster)
			Expect(err).To(HaveOccurred())
			Expect(cidr).To(Equal(""))
		})

		It("No Match", func() {
			cluster := createCluster("1.2.5.200", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.6.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(cluster)
			Expect(err).To(HaveOccurred())
			Expect(cidr).To(Equal(""))
		})
		It("Bad inventory", func() {
			cluster := createCluster("1.2.5.6", "",
				"Bad inventory",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			cidr, err := CalculateMachineNetworkCIDR(cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(cidr).To(Equal("1.2.4.0/23"))
		})
	})
	Context("GetMachineCIDRHosts", func() {
		It("No Machine CIDR", func() {
			cluster := createCluster("1.2.5.6", "",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			_, err := GetMachineCIDRHosts(cluster)
			Expect(err).To(HaveOccurred())
		})
		It("No matching Machine CIDR", func() {
			cluster := createCluster("1.2.5.6", "1.1.0.0/16",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")))
			hosts, err := GetMachineCIDRHosts(cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(hosts).To(BeEmpty())
		})
		It("Some matched", func() {
			cluster := createCluster("1.2.5.6", "1.2.4.0/23",
				createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
				createInventory(createInterface("127.0.0.1/17")),
				createInventory(createInterface("1.2.4.79/23")))
			hosts, err := GetMachineCIDRHosts(cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(hosts).To(Equal([]*models.Host{
				cluster.Hosts[0],
				cluster.Hosts[2],
			}))

		})
	})
})
