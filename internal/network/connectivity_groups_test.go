package network

import (
	"encoding/json"
	"fmt"
	"net"

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

func linkNet1(n *node) *models.L2Connectivity {
	return &models.L2Connectivity{
		RemoteIPAddress: n.addressNet1,
		Successful:      true,
	}
}

func linkNet2(n *node) *models.L2Connectivity {
	return &models.L2Connectivity{
		RemoteIPAddress: n.addressNet2,
		Successful:      true,
	}
}

func unLinkNet1(n *node) *models.L2Connectivity {
	return &models.L2Connectivity{
		RemoteIPAddress: n.addressNet1,
		Successful:      false,
	}
}

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

func createRemote(remote *node, connFuncs ...func(h *node) *models.L2Connectivity) *models.ConnectivityRemoteHost {

	l2s := make([]*models.L2Connectivity, 0)
	for _, f := range connFuncs {
		l2s = append(l2s, f(remote))
	}

	return &models.ConnectivityRemoteHost{
		HostID:         *remote.id,
		L2Connectivity: l2s,
	}
}

var _ = Describe("connectivity groups all", func() {
	GenerateConnectivityGroupTests(true, "1.2.3.0/24", "2.2.3.0/24")
	GenerateConnectivityGroupTests(false, "2001:db8::/120", "fe80:5054::/120")
})

func GenerateConnectivityGroupTests(ipV4 bool, net1CIDR, net2CIDR string) {

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

		createConnectivityReport := func(remoteHosts ...*models.ConnectivityRemoteHost) string {
			report := models.ConnectivityReport{
				RemoteHosts: remoteHosts,
			}
			b, err := json.Marshal(&report)
			Expect(err).ToNot(HaveOccurred())
			return string(b)
		}

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
				ret, err := CreateMajorityGroup(net1CIDR, hosts)
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
				ret, err := CreateMajorityGroup(net1CIDR, hosts)
				Expect(err).ToNot(HaveOccurred())
				Expect(ret).To(Equal([]strfmt.UUID{}))
			})
		})
		It("One with data", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[1], linkNet1),
						createRemote(nodes[2], linkNet1)),
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
			ret, err := CreateMajorityGroup(net1CIDR, hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(Equal([]strfmt.UUID{}))
		})
		It("3 with data", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[1], linkNet1),
						createRemote(nodes[2], linkNet1)),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[0], linkNet1),
						createRemote(nodes[2], linkNet1)),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[0], linkNet1),
						createRemote(nodes[1], linkNet1)),
				},
			}
			ret, err := CreateMajorityGroup(net1CIDR, hosts)
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
						createRemote(nodes[1], linkNet1),
						createRemote(nodes[2], linkNet1)),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[0], linkNet1),
						createRemote(nodes[2], linkNet1)),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[0], linkNet2),
						createRemote(nodes[1], linkNet2)),
				},
			}
			ret, err := CreateMajorityGroup(net1CIDR, hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(Equal([]strfmt.UUID{}))
		})
		It("3 with data, additional network", func() {
			hosts := []*models.Host{
				{
					ID: nodes[0].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[1], linkNet1, linkNet2),
						createRemote(nodes[2], linkNet1),
						createRemote(nodes[3], linkNet2),
					),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[0], linkNet1, linkNet2),
						createRemote(nodes[2], linkNet1),
						createRemote(nodes[3], linkNet2),
					),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[0], linkNet1, linkNet2),
						createRemote(nodes[1], linkNet1, linkNet2)),
				},
				{
					ID: nodes[3].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[0], linkNet2),
						createRemote(nodes[1], linkNet2)),
				},
			}
			ret, err := CreateMajorityGroup(net1CIDR, hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(3))
			Expect(ret).To(ContainElement(*nodes[0].id))
			Expect(ret).To(ContainElement(*nodes[1].id))
			Expect(ret).To(ContainElement(*nodes[2].id))
			ret, err = CreateMajorityGroup(net2CIDR, hosts)
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
						createRemote(nodes[1], linkNet1),
						createRemote(nodes[2], linkNet1)),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[0], linkNet1),
						createRemote(nodes[2], linkNet1)),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[0], linkNet1),
						createRemote(nodes[1], linkNet1)),
				},
				{
					ID: nodes[3].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[4], linkNet1),
						createRemote(nodes[5], linkNet1),
						createRemote(nodes[6], linkNet1),
					),
				},
				{
					ID: nodes[4].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[3], linkNet1),
						createRemote(nodes[5], linkNet1),
						createRemote(nodes[6], linkNet1)),
				},
				{
					ID: nodes[5].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[3], linkNet1),
						createRemote(nodes[4], linkNet1),
						createRemote(nodes[6], linkNet1)),
				},
				{
					ID: nodes[6].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[3], linkNet1),
						createRemote(nodes[4], linkNet1),
						createRemote(nodes[5], linkNet1)),
				},
			}
			ret, err := CreateMajorityGroup(net1CIDR, hosts)
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
						createRemote(nodes[1], linkNet1),
						createRemote(nodes[2], linkNet1)),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[0], linkNet1),
						createRemote(nodes[2], linkNet1)),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[0], linkNet1),
						createRemote(nodes[1], linkNet1)),
				},
				{
					ID: nodes[3].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[4], linkNet1),
						createRemote(nodes[5], linkNet1),
						createRemote(nodes[6], linkNet1),
					),
				},
				{
					ID: nodes[4].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[3], linkNet1),
						createRemote(nodes[5], linkNet1),
						createRemote(nodes[6], linkNet1)),
				},
				{
					ID: nodes[5].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[3], linkNet1),
						createRemote(nodes[4], linkNet1),
						createRemote(nodes[6], linkNet1)),
				},
				{
					ID: nodes[6].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[3], linkNet1),
						createRemote(nodes[4], linkNet1),
						createRemote(nodes[5], unLinkNet1)),
				},
			}
			ret, err := CreateMajorityGroup(net1CIDR, hosts)
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
						createRemote(nodes[1], linkNet1),
						createRemote(nodes[2], linkNet1)),
				},
				{
					ID: nodes[1].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[0], linkNet1),
						createRemote(nodes[2], linkNet1)),
				},
				{
					ID: nodes[2].id,
					Connectivity: createConnectivityReport(createRemote(nodes[0], linkNet1),
						createRemote(nodes[1], unLinkNet1)),
				},
				{
					ID: nodes[3].id,
					Connectivity: createConnectivityReport(createRemote(nodes[4], linkNet1),
						createRemote(nodes[5], linkNet1),
						createRemote(nodes[6], linkNet1),
					),
				},
				{
					ID: nodes[4].id,
					Connectivity: createConnectivityReport(createRemote(nodes[3], linkNet1),
						createRemote(nodes[5], linkNet1),
						createRemote(nodes[6], linkNet1)),
				},
				{
					ID: nodes[5].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[3], linkNet1),
						createRemote(nodes[4], linkNet1),
						createRemote(nodes[6], linkNet1)),
				},
				{
					ID: nodes[6].id,
					Connectivity: createConnectivityReport(
						createRemote(nodes[3], linkNet1),
						createRemote(nodes[4], linkNet1),
						createRemote(nodes[5], unLinkNet1)),
				},
			}
			ret, err := CreateMajorityGroup(net1CIDR, hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(HaveLen(3))
			Expect(ret).To(ContainElement(*nodes[3].id))
			Expect(ret).To(ContainElement(*nodes[4].id))
			Expect(ret).To(ContainElement(*nodes[5].id))
		})
	})
}
