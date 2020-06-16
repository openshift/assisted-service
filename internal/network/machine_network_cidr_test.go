package network

import (
	"encoding/json"
	"testing"

	"github.com/filanov/bm-inventory/internal/common"

	"github.com/sirupsen/logrus"

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

	createCluster := func(apiVip string, machineCidr string, inventories ...string) *common.Cluster {
		return &common.Cluster{Cluster: models.Cluster{
			APIVip:             apiVip,
			MachineNetworkCidr: machineCidr,
			Hosts:              createHosts(inventories...),
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
		It("Same vips", func() {
			cluster := createCluster("1.2.5.6", "1.2.4.0/23",
				createInventory(createInterface("1.2.5.7/23")))
			cluster.IngressVip = cluster.APIVip
			err := VerifyVips(cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, false)
			Expect(err).To(HaveOccurred())
			err = VerifyVips(cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, true)
			Expect(err).To(HaveOccurred())
		})
		It("Different vips", func() {
			cluster := createCluster("1.2.5.6", "1.2.4.0/23",
				createInventory(createInterface("1.2.5.7/23")))
			cluster.IngressVip = "1.2.5.8"
			err := VerifyVips(cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, false)
			Expect(err).ToNot(HaveOccurred())
			err = VerifyVips(cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip, true)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func TestMachineNetworkCidr(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Machine network cider Suite")
}
