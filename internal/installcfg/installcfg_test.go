package installcfg

import (
	"encoding/json"
	"testing"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
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

	createCluster := func(apiVip string, inventories ...string) *models.Cluster {
		return &models.Cluster{
			APIVip: strfmt.IPv4(apiVip),
			Hosts:  createHosts(inventories...),
		}
	}

	It("happpy flow", func() {
		cluster := createCluster("1.2.5.6",
			createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
			createInventory(createInterface("127.0.0.1/17")))
		cidr, err := getMachineCIDR(cluster)
		Expect(err).To(Not(HaveOccurred()))
		Expect(cidr).To(Equal("1.2.4.0/23"))
	})

	It("Illegal VIP", func() {
		cluster := createCluster("1.2.5.257",
			createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
			createInventory(createInterface("127.0.0.1/17")))
		cidr, err := getMachineCIDR(cluster)
		Expect(err).To(HaveOccurred())
		Expect(cidr).To(Equal(""))
	})

	It("No Match", func() {
		cluster := createCluster("1.2.5.200",
			createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.6.7/23")),
			createInventory(createInterface("127.0.0.1/17")))
		cidr, err := getMachineCIDR(cluster)
		Expect(err).To(HaveOccurred())
		Expect(cidr).To(Equal(""))
	})
	It("Bad inventory", func() {
		cluster := createCluster("1.2.5.6",
			"Bad inventory",
			createInventory(createInterface("3.3.3.3/16"), createInterface("8.8.8.8/8", "1.2.5.7/23")),
			createInventory(createInterface("127.0.0.1/17")))
		cidr, err := getMachineCIDR(cluster)
		Expect(err).To(Not(HaveOccurred()))
		Expect(cidr).To(Equal("1.2.4.0/23"))
	})
})

func TestSubsystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "host state machine tests")
}
