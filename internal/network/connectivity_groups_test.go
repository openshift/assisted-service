package network

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

type node struct {
	id          *strfmt.UUID
	addressNet1 string
	addressNet2 string
}

func l2LinkNet1(n *node) *models.L2Connectivity {
	return &models.L2Connectivity{
		RemoteIPAddress: n.addressNet1,
		Successful:      true,
	}
}

func l2LinkNet2(n *node) *models.L2Connectivity {
	return &models.L2Connectivity{
		RemoteIPAddress: n.addressNet2,
		Successful:      true,
	}
}

func unL2LinkNet1(n *node) *models.L2Connectivity {
	return &models.L2Connectivity{
		RemoteIPAddress: n.addressNet1,
		Successful:      false,
	}
}

func l3LinkNet1(n *node) *models.L3Connectivity {
	return &models.L3Connectivity{
		RemoteIPAddress: n.addressNet1,
		Successful:      true,
	}
}

func l3LinkNet2(n *node) *models.L3Connectivity {
	return &models.L3Connectivity{
		RemoteIPAddress: n.addressNet2,
		Successful:      true,
	}
}

//func unL3LinkNet1(n *node) *models.L3Connectivity {
//	return &models.L3Connectivity{
//		RemoteIPAddress: n.addressNet1,
//		Successful:      false,
//	}
//}

func generateIPv4Nodes(count int, net1CIDR, net2CIDR string) []*node {

	net1, _, _ := net.ParseCIDR(net1CIDR)
	net1Address := net1.To4()
	net1Address[3] += 4

	net2, _, _ := net.ParseCIDR(net2CIDR)
	net2Address := net2.To4()
	net2Address[3] += 4

	ret := make([]*node, count)
	for i := 0; i < count; i++ {
		id := strfmt.UUID(uuid.New().String())
		ret[i] = &node{
			id:          &id,
			addressNet1: net1Address.String(),
			addressNet2: net2Address.String(),
		}

		net1Address[3]++
		net2Address[3]++
	}

	return ret
}

func generateIPv6Nodes(count int, net1CIDR, net2CIDR string) []*node {

	net1, _, _ := net.ParseCIDR(net1CIDR)
	net1Address := net1.To16()
	net1Address[15] += 4

	net2, _, _ := net.ParseCIDR(net2CIDR)
	net2Address := net2.To16()
	net2Address[15] += 4

	ret := make([]*node, count)
	for i := 0; i < count; i++ {
		id := strfmt.UUID(uuid.New().String())
		ret[i] = &node{
			id:          &id,
			addressNet1: net1Address.String(),
			addressNet2: net2Address.String(),
		}
		net1Address[15]++
		net2Address[15]++
	}

	return ret
}

func createL2Remote(remote *node, connFuncs ...func(h *node) *models.L2Connectivity) *models.ConnectivityRemoteHost {

	l2s := make([]*models.L2Connectivity, 0)
	for _, f := range connFuncs {
		l2s = append(l2s, f(remote))
	}

	return &models.ConnectivityRemoteHost{
		HostID:         *remote.id,
		L2Connectivity: l2s,
	}
}

func createL3Remote(remote *node, connFuncs ...func(h *node) *models.L3Connectivity) *models.ConnectivityRemoteHost {

	l2s := make([]*models.L3Connectivity, 0)
	for _, f := range connFuncs {
		l2s = append(l2s, f(remote))
	}

	return &models.ConnectivityRemoteHost{
		HostID:         *remote.id,
		L3Connectivity: l2s,
	}
}

func createConnectivityReport(remoteHosts ...*models.ConnectivityRemoteHost) string {
	report := models.ConnectivityReport{
		RemoteHosts: remoteHosts,
	}
	b, err := json.Marshal(&report)
	Expect(err).ToNot(HaveOccurred())
	return string(b)
}

func makeInventory(node *node) string {
	var inventory models.Inventory
	for i, a := range []string{node.addressNet1, node.addressNet2} {
		if a == "" {
			continue
		}
		name := fmt.Sprintf("eth%d", i)
		newInterface := &models.Interface{
			Name: name,
		}
		if strings.Contains(a, ":") {
			newInterface.IPV6Addresses = append(newInterface.IPV6Addresses, a+"/64")
		} else {
			newInterface.IPV4Addresses = append(newInterface.IPV4Addresses, a+"/24")
		}
		inventory.Interfaces = append(inventory.Interfaces, newInterface)
	}
	b, err := json.Marshal(&inventory)
	Expect(err).ToNot(HaveOccurred())
	return string(b)
}

var _ = Describe("L2 connectivity groups all", func() {
	GenerateL2ConnectivityGroupTests(true, "1.2.3.0/24", "2.2.3.0/24")
	GenerateL2ConnectivityGroupTests(false, "2001:db8::/120", "fe80:5054::/120")
})

func GenerateL2ConnectivityGroupTests(ipV4 bool, net1CIDR, net2CIDR string) {

	var ipVersion string
	var nodes []*node
	if ipV4 {
		ipVersion = "IPv4"
		nodes = generateIPv4Nodes(7, net1CIDR, net2CIDR)
	} else {
		ipVersion = "IPv6"
		nodes = generateIPv6Nodes(7, net1CIDR, net2CIDR)
	}

	Describe(fmt.Sprintf("connectivity groups %s", ipVersion), func() {

		Context("connectivity groups", func() {
			It("Empty", func() {
				hosts := []*models.Host{
					{
						ID:           nodes[0].id,
						Connectivity: createConnectivityReport(),
					},
					{
						ID:           nodes[1].id,
						Connectivity: createConnectivityReport(),
					},
					{
						ID:           nodes[2].id,
						Connectivity: createConnectivityReport(),
					},
				}
				ret, err := CreateL2MajorityGroup(net1CIDR, hosts)
				Expect(err).ToNot(HaveOccurred())
				Expect(ret).To(Equal([]strfmt.UUID{}))
			})
			It("Empty 2", func() {
				hosts := []*models.Host{
					{
						ID: nodes[0].id,
					},
					{
						ID: nodes[1].id,
					},
					{
						ID: nodes[2].id,
					},
				}
				ret, err := CreateL2MajorityGroup(net1CIDR, hosts)
				Expect(err).ToNot(HaveOccurred())
				Expect(ret).To(Equal([]strfmt.UUID{}))
			})
		})
		It("One with data", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[1], l2LinkNet1),
						createL2Remote(nodes[2], l2LinkNet1)),
				},
				{
					ID:           nodes[1].id,
					Connectivity: createConnectivityReport(),
				},
				{
					ID:           nodes[2].id,
					Connectivity: createConnectivityReport(),
				},
			}
			ret, err := CreateL2MajorityGroup(net1CIDR, hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(Equal([]strfmt.UUID{}))
		})
		It("3 with data", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[1], l2LinkNet1),
						createL2Remote(nodes[2], l2LinkNet1)),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[0], l2LinkNet1),
						createL2Remote(nodes[2], l2LinkNet1)),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[0], l2LinkNet1),
						createL2Remote(nodes[1], l2LinkNet1)),
				},
			}
			ret, err := CreateL2MajorityGroup(net1CIDR, hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(3))
			Expect(ret).To(ContainElement(*nodes[0].id))
			Expect(ret).To(ContainElement(*nodes[1].id))
			Expect(ret).To(ContainElement(*nodes[2].id))
		})
		It("Different network", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[1], l2LinkNet1),
						createL2Remote(nodes[2], l2LinkNet1)),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[0], l2LinkNet1),
						createL2Remote(nodes[2], l2LinkNet1)),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[0], l2LinkNet2),
						createL2Remote(nodes[1], l2LinkNet2)),
				},
			}
			ret, err := CreateL2MajorityGroup(net1CIDR, hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(Equal([]strfmt.UUID{}))
		})
		It("3 with data, additional network", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[1], l2LinkNet1, l2LinkNet2),
						createL2Remote(nodes[2], l2LinkNet1),
						createL2Remote(nodes[3], l2LinkNet2),
					),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[0], l2LinkNet1, l2LinkNet2),
						createL2Remote(nodes[2], l2LinkNet1),
						createL2Remote(nodes[3], l2LinkNet2),
					),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[0], l2LinkNet1, l2LinkNet2),
						createL2Remote(nodes[1], l2LinkNet1, l2LinkNet2)),
				},
				{
					ID: nodes[3].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[0], l2LinkNet2),
						createL2Remote(nodes[1], l2LinkNet2)),
				},
			}
			ret, err := CreateL2MajorityGroup(net1CIDR, hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(3))
			Expect(ret).To(ContainElement(*nodes[0].id))
			Expect(ret).To(ContainElement(*nodes[1].id))
			Expect(ret).To(ContainElement(*nodes[2].id))
			ret, err = CreateL2MajorityGroup(net2CIDR, hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(3))
			Expect(ret).To(ContainElement(*nodes[0].id))
			Expect(ret).To(ContainElement(*nodes[1].id))
			Expect(ret).To(ContainElement(*nodes[3].id))
		})
		It("7 - 2 groups", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[1], l2LinkNet1),
						createL2Remote(nodes[2], l2LinkNet1)),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[0], l2LinkNet1),
						createL2Remote(nodes[2], l2LinkNet1)),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[0], l2LinkNet1),
						createL2Remote(nodes[1], l2LinkNet1)),
				},
				{
					ID: nodes[3].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[4], l2LinkNet1),
						createL2Remote(nodes[5], l2LinkNet1),
						createL2Remote(nodes[6], l2LinkNet1),
					),
				},
				{
					ID: nodes[4].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[3], l2LinkNet1),
						createL2Remote(nodes[5], l2LinkNet1),
						createL2Remote(nodes[6], l2LinkNet1)),
				},
				{
					ID: nodes[5].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[3], l2LinkNet1),
						createL2Remote(nodes[4], l2LinkNet1),
						createL2Remote(nodes[6], l2LinkNet1)),
				},
				{
					ID: nodes[6].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[3], l2LinkNet1),
						createL2Remote(nodes[4], l2LinkNet1),
						createL2Remote(nodes[5], l2LinkNet1)),
				},
			}
			ret, err := CreateL2MajorityGroup(net1CIDR, hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(4))
			Expect(ret).To(ContainElement(*nodes[3].id))
			Expect(ret).To(ContainElement(*nodes[4].id))
			Expect(ret).To(ContainElement(*nodes[5].id))
			Expect(ret).To(ContainElement(*nodes[6].id))
		})
		It("7 - 1 direction missing", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[1], l2LinkNet1),
						createL2Remote(nodes[2], l2LinkNet1)),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[0], l2LinkNet1),
						createL2Remote(nodes[2], l2LinkNet1)),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[0], l2LinkNet1),
						createL2Remote(nodes[1], l2LinkNet1)),
				},
				{
					ID: nodes[3].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[4], l2LinkNet1),
						createL2Remote(nodes[5], l2LinkNet1),
						createL2Remote(nodes[6], l2LinkNet1),
					),
				},
				{
					ID: nodes[4].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[3], l2LinkNet1),
						createL2Remote(nodes[5], l2LinkNet1),
						createL2Remote(nodes[6], l2LinkNet1)),
				},
				{
					ID: nodes[5].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[3], l2LinkNet1),
						createL2Remote(nodes[4], l2LinkNet1),
						createL2Remote(nodes[6], l2LinkNet1)),
				},
				{
					ID: nodes[6].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[3], l2LinkNet1),
						createL2Remote(nodes[4], l2LinkNet1),
						createL2Remote(nodes[5], unL2LinkNet1)),
				},
			}
			ret, err := CreateL2MajorityGroup(net1CIDR, hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(3))
			Expect(ret).To(ContainElement(*nodes[0].id))
			Expect(ret).To(ContainElement(*nodes[1].id))
			Expect(ret).To(ContainElement(*nodes[2].id))
		})
		It("7 - 2 directions missing", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[1], l2LinkNet1),
						createL2Remote(nodes[2], l2LinkNet1)),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[0], l2LinkNet1),
						createL2Remote(nodes[2], l2LinkNet1)),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(createL2Remote(nodes[0], l2LinkNet1),
						createL2Remote(nodes[1], unL2LinkNet1)),
				},
				{
					ID: nodes[3].id,
					Connectivity: createConnectivityReport(createL2Remote(nodes[4], l2LinkNet1),
						createL2Remote(nodes[5], l2LinkNet1),
						createL2Remote(nodes[6], l2LinkNet1),
					),
				},
				{
					ID: nodes[4].id,
					Connectivity: createConnectivityReport(createL2Remote(nodes[3], l2LinkNet1),
						createL2Remote(nodes[5], l2LinkNet1),
						createL2Remote(nodes[6], l2LinkNet1)),
				},
				{
					ID: nodes[5].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[3], l2LinkNet1),
						createL2Remote(nodes[4], l2LinkNet1),
						createL2Remote(nodes[6], l2LinkNet1)),
				},
				{
					ID: nodes[6].id,
					Connectivity: createConnectivityReport(
						createL2Remote(nodes[3], l2LinkNet1),
						createL2Remote(nodes[4], l2LinkNet1),
						createL2Remote(nodes[5], unL2LinkNet1)),
				},
			}
			ret, err := CreateL2MajorityGroup(net1CIDR, hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(3))
			Expect(ret).To(ContainElement(*nodes[3].id))
			Expect(ret).To(ContainElement(*nodes[4].id))
			Expect(ret).To(ContainElement(*nodes[5].id))
		})
	})
}

var _ = Describe("L2 Ipv6 with L3 fallback", func() {
	var (
		nodes    []*node
		net1CIDR string
		hosts    []*models.Host
	)

	BeforeEach(func() {
		net1CIDR = "2001:db8::/120"
		nodes = generateIPv6Nodes(7, "2001:db8::/120", "fe80:5054::/120")
		hosts = []*models.Host{
			{
				ID: nodes[0].id,
				Connectivity: createConnectivityReport(
					createL2Remote(nodes[1], l2LinkNet1),
					createL2Remote(nodes[2], l2LinkNet1)),
			},
			{
				ID: nodes[1].id,
				Connectivity: createConnectivityReport(
					createL2Remote(nodes[0], l2LinkNet1),
					createL2Remote(nodes[2], l2LinkNet1)),
			},
			{
				ID: nodes[2].id,
				Connectivity: createConnectivityReport(
					createL2Remote(nodes[0], l2LinkNet1)),
			},
		}
	})
	It("Missing L2 connectivity", func() {
		ret, err := CreateL2MajorityGroup(net1CIDR, hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(ret).To(HaveLen(0))
	})

	It("Missing L2 connectivity. Add L3 connectivity", func() {
		hosts[2].Connectivity = createConnectivityReport(
			createL2Remote(nodes[0], l2LinkNet1),
			createL3Remote(nodes[1], l3LinkNet1))
		ret, err := CreateL2MajorityGroup(net1CIDR, hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(ret).To(HaveLen(3))
		Expect(ret).To(ContainElement(*nodes[0].id))
		Expect(ret).To(ContainElement(*nodes[1].id))
		Expect(ret).To(ContainElement(*nodes[2].id))
	})
})

var _ = Describe("L3 connectivity groups all", func() {
	GenerateL3ConnectivityGroupTests(true, "1.2.3.0/24", "2.2.3.0/24")
	GenerateL3ConnectivityGroupTests(false, "2001:db8::/120", "fe80:5054::/120")
})

func GenerateL3ConnectivityGroupTests(ipV4 bool, net1CIDR, net2CIDR string) {

	var ipVersion string
	var nodes []*node
	var family AddressFamily
	if ipV4 {
		family = IPv4
	} else {
		family = IPv6
	}
	Describe(fmt.Sprintf("connectivity groups %s", ipVersion), func() {
		BeforeEach(func() {
			if ipV4 {
				ipVersion = "IPv4"
				nodes = generateIPv4Nodes(7, net1CIDR, net2CIDR)
			} else {
				ipVersion = "IPv6"
				nodes = generateIPv6Nodes(7, net1CIDR, net2CIDR)
			}
		})

		Context("connectivity groups", func() {
			It("Empty", func() {
				hosts := []*models.Host{
					{
						ID:           nodes[0].id,
						Connectivity: createConnectivityReport(),
						Inventory:    makeInventory(nodes[0]),
					},
					{
						ID:           nodes[1].id,
						Connectivity: createConnectivityReport(),
						Inventory:    makeInventory(nodes[1]),
					},
					{
						ID:           nodes[2].id,
						Connectivity: createConnectivityReport(),
						Inventory:    makeInventory(nodes[2]),
					},
				}
				ret, err := CreateL3MajorityGroup(hosts, family)
				Expect(err).ToNot(HaveOccurred())
				Expect(ret).To(Equal([]strfmt.UUID{}))
			})
			It("Empty 2", func() {
				hosts := []*models.Host{
					{
						ID:        nodes[0].id,
						Inventory: makeInventory(nodes[0]),
					},
					{
						ID:        nodes[1].id,
						Inventory: makeInventory(nodes[1]),
					},
					{
						ID:        nodes[2].id,
						Inventory: makeInventory(nodes[2]),
					},
				}
				ret, err := CreateL3MajorityGroup(hosts, family)
				Expect(err).ToNot(HaveOccurred())
				Expect(ret).To(Equal([]strfmt.UUID{}))
			})
		})
		It("One with data", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[1], l3LinkNet1),
						createL3Remote(nodes[2], l3LinkNet1)),
					Inventory: makeInventory(nodes[0]),
				},
				{
					ID:           nodes[1].id,
					Connectivity: createConnectivityReport(),
					Inventory:    makeInventory(nodes[1]),
				},
				{
					ID:           nodes[2].id,
					Connectivity: createConnectivityReport(),
					Inventory:    makeInventory(nodes[2]),
				},
			}
			ret, err := CreateL3MajorityGroup(hosts, family)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(Equal([]strfmt.UUID{}))
		})
		It("3 with data", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[1], l3LinkNet1),
						createL3Remote(nodes[2], l3LinkNet1)),
					Inventory: makeInventory(nodes[0]),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1),
						createL3Remote(nodes[2], l3LinkNet1)),
					Inventory: makeInventory(nodes[1]),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1),
						createL3Remote(nodes[1], l3LinkNet1)),
					Inventory: makeInventory(nodes[2]),
				},
			}
			ret, err := CreateL3MajorityGroup(hosts, family)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(0))
		})
		It("3 with data - two networks", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[1], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[0]),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[1]),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[1], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[2]),
				},
			}
			ret, err := CreateL3MajorityGroup(hosts, family)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(3))
			Expect(ret).To(ContainElement(*nodes[0].id))
			Expect(ret).To(ContainElement(*nodes[1].id))
			Expect(ret).To(ContainElement(*nodes[2].id))
		})
		It("3 with data - two networks, one ip missing", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[1], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[0]),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[1]),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[1], l3LinkNet1)),
					Inventory: makeInventory(nodes[2]),
				},
			}
			ret, err := CreateL3MajorityGroup(hosts, family)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(0))
		})
		It("4 with data - two networks", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[1], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[3], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[0]),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[3], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[1]),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[1], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[3], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[2]),
				},
				{
					ID: nodes[3].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[1], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[3]),
				},
			}
			ret, err := CreateL3MajorityGroup(hosts, family)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(4))
			Expect(ret).To(ContainElement(*nodes[0].id))
			Expect(ret).To(ContainElement(*nodes[1].id))
			Expect(ret).To(ContainElement(*nodes[2].id))
			Expect(ret).To(ContainElement(*nodes[3].id))
		})
		It("4 with data - two networks - one with single network", func() {
			nodes[3].addressNet2 = ""
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[1], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[3], l3LinkNet1)),
					Inventory: makeInventory(nodes[0]),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[3], l3LinkNet1)),
					Inventory: makeInventory(nodes[1]),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[1], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[3], l3LinkNet1)),
					Inventory: makeInventory(nodes[2]),
				},
				{
					ID: nodes[3].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[1], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[3]),
				},
			}
			ret, err := CreateL3MajorityGroup(hosts, family)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(4))
			Expect(ret).To(ContainElement(*nodes[0].id))
			Expect(ret).To(ContainElement(*nodes[1].id))
			Expect(ret).To(ContainElement(*nodes[2].id))
			Expect(ret).To(ContainElement(*nodes[3].id))
		})
		It("4 with data - two networks, 1 disconnected", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[1], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[3], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[0]),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[3], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[1]),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[0], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[1], l3LinkNet1, l3LinkNet2),
						createL3Remote(nodes[3], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[2]),
				},
				{
					ID: nodes[3].id,
					Connectivity: createConnectivityReport(
						createL3Remote(nodes[2], l3LinkNet1, l3LinkNet2)),
					Inventory: makeInventory(nodes[3]),
				},
			}
			ret, err := CreateL3MajorityGroup(hosts, family)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(3))
			Expect(ret).To(ContainElement(*nodes[0].id))
			Expect(ret).To(ContainElement(*nodes[1].id))
			Expect(ret).To(ContainElement(*nodes[2].id))
		})
	})
}
