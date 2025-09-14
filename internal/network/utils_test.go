package network

import (
	"fmt"
	"net"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var mNetworks = []*net.IPNet{
	{IP: net.IP{192, 168, 10, 0}, Mask: net.IPv4Mask(255, 255, 255, 0)},
	{IP: net.IP{192, 168, 11, 0}, Mask: net.IPv4Mask(255, 255, 255, 0)},
}

var _ = Describe("host IP address families", func() {
	It("host doesn't have interfaces", func() {
		host := &models.Host{
			Inventory: "{}",
		}
		v4, v6, err := GetHostAddressFamilies(host)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeFalse())
		Expect(v6).To(BeFalse())
	})
	It("error parsing inventory", func() {
		host := &models.Host{
			Inventory: "",
		}
		_, _, err := GetHostAddressFamilies(host)
		Expect(err).Should(HaveOccurred())
	})
	It("host has only IPv4 addresses", func() {
		host := &models.Host{
			Inventory: `{
				"interfaces":[
					{
						"ipv6_addresses":[],
						"ipv4_addresses":[
							"192.186.10.12/24"
						]
					}
				]
			}`,
		}
		v4, v6, err := GetHostAddressFamilies(host)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeTrue())
		Expect(v6).To(BeFalse())
	})
	It("host has only IPv6 addresses", func() {
		host := &models.Host{
			Inventory: `{
				"interfaces":
				[
					{
						"ipv6_addresses":[
							"2002:db8::2/64"
						],
						"ipv4_addresses":[]
					}
				]
			}`,
		}
		v4, v6, err := GetHostAddressFamilies(host)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeFalse())
		Expect(v6).To(BeTrue())
	})
	It("host has both IPv4 and IPv6 addresses on same interface", func() {
		host := &models.Host{
			Inventory: `{"interfaces":
				[
					{
						"ipv4_addresses":[
							"192.186.10.12/24"
						],
						"ipv6_addresses":[
							"2002:db8::1/64"
						]
					}
				]
			}`,
		}
		v4, v6, err := GetHostAddressFamilies(host)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeTrue())
		Expect(v6).To(BeTrue())
	})
	It("host has both IPv4 and IPv6 addresses on different interfaces", func() {
		host := &models.Host{
			Inventory: `{
				"interfaces":[
					{
						"ipv4_addresses":[
							"192.186.10.12/24"
						]
					},
					{
						"ipv6_addresses":[
							"2002:db8::1/64"
						]
					}
				]
			}`,
		}
		v4, v6, err := GetHostAddressFamilies(host)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeTrue())
		Expect(v6).To(BeTrue())
	})
	It("host has both IPv4 and IPv6 addresses on different interfaces, reverse order", func() {
		host := &models.Host{
			Inventory: `{
				"interfaces":[
					{
						"ipv6_addresses":[
							"2002:db8::1/64"
						]
					},
					{
						"ipv4_addresses":[
							"192.186.10.12/24"
						]
					}
				]
			}`,
		}
		v4, v6, err := GetHostAddressFamilies(host)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeTrue())
		Expect(v6).To(BeTrue())
	})
})

var _ = Describe("cluster IP address families", func() {
	It("cluster doesn't have hosts", func() {
		hosts := []*models.Host{}
		v4, v6, err := GetClusterAddressStack(hosts)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeFalse())
		Expect(v6).To(BeFalse())
	})
	It("error parsing inventory", func() {
		hosts := []*models.Host{
			{
				Inventory: "",
			},
		}
		_, _, err := GetClusterAddressStack(hosts)
		Expect(err).Should(HaveOccurred())
	})
	It("cluster has hosts with only IPv4 addresses", func() {
		hosts := []*models.Host{
			{
				Inventory: `{
					"interfaces":[
						{
							"ipv6_addresses":[],
							"ipv4_addresses":[
								"192.186.10.12/24"
							]
						}
					]
				}`,
			},
			{
				Inventory: `{
					"interfaces":[
						{
							"ipv6_addresses":[],
							"ipv4_addresses":[
								"192.186.10.14/24"
							]
						}
					]
				}`,
			},
		}
		v4, v6, err := GetClusterAddressStack(hosts)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeTrue())
		Expect(v6).To(BeFalse())
	})
	It("cluster has hosts with only IPv6 addresses", func() {
		hosts := []*models.Host{
			{
				Inventory: `{
					"interfaces":
					[
						{
							"ipv6_addresses":[
								"2002:db8::2/64"
							],
							"ipv4_addresses":[]
						}
					]
				}`,
			},
			{
				Inventory: `{
					"interfaces":
					[
						{
							"ipv6_addresses":[
								"2002:db8::4/64"
							],
							"ipv4_addresses":[]
						}
					]
				}`,
			},
		}
		v4, v6, err := GetClusterAddressStack(hosts)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeFalse())
		Expect(v6).To(BeTrue())
	})
	It("cluster has one host with IPv4 and one host with IPv6 addresses", func() {
		hosts := []*models.Host{
			{
				Inventory: `{"interfaces":
					[
						{
							"ipv4_addresses":[
								"192.186.10.12/24"
							],
							"ipv6_addresses":[]
						}
					]
				}`,
			},
			{
				Inventory: `{"interfaces":
					[
						{
							"ipv4_addresses":[],
							"ipv6_addresses":[
								"2002:db8::1/64"
							]
						}
					]
				}`,
			},
		}
		v4, v6, err := GetClusterAddressStack(hosts)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeFalse())
		Expect(v6).To(BeFalse())
	})
	It("cluster has one host with IPv4 and one host with dual stack", func() {
		hosts := []*models.Host{
			{
				Inventory: `{"interfaces":
					[
						{
							"ipv4_addresses":[
								"192.186.10.12/24"
							],
							"ipv6_addresses":[]
						}
					]
				}`,
			},
			{
				Inventory: `{"interfaces":
					[
						{
							"ipv4_addresses":[
								"192.186.10.14/24"
							],
							"ipv6_addresses":[
								"2002:db8::1/64"
							]
						}
					]
				}`,
			},
		}
		v4, v6, err := GetClusterAddressStack(hosts)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeTrue())
		Expect(v6).To(BeFalse())
	})
	It("cluster has one host with IPv6 and one host with dual stack", func() {
		hosts := []*models.Host{
			{
				Inventory: `{"interfaces":
					[
						{
							"ipv4_addresses":[],
							"ipv6_addresses":[
								"2002:db8::4/64"
							]
						}
					]
				}`,
			},
			{
				Inventory: `{"interfaces":
					[
						{
							"ipv4_addresses":[
								"192.186.10.14/24"
							],
							"ipv6_addresses":[
								"2002:db8::4/64"
							]
						}
					]
				}`,
			},
		}
		v4, v6, err := GetClusterAddressStack(hosts)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeFalse())
		Expect(v6).To(BeTrue())
	})
	It("cluster has hosts with both an IPv4 and an IPv6 address each", func() {
		hosts := []*models.Host{
			{
				Inventory: `{
					"interfaces":[
						{
							"ipv4_addresses":[
								"192.186.10.12/24"
							]
						},
						{
							"ipv6_addresses":[
								"2002:db8::1/64"
							]
						}
					]
				}`,
			},
			{
				Inventory: `{
					"interfaces":[
						{
							"ipv4_addresses":[
								"192.186.10.14/24"
							]
						},
						{
							"ipv6_addresses":[
								"2002:db8::4/64"
							]
						}
					]
				}`,
			},
		}
		v4, v6, err := GetClusterAddressStack(hosts)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(v4).To(BeTrue())
		Expect(v6).To(BeTrue())
	})
})

var _ = Describe("AreMachineNetworksIdentical", func() {
	tests := []struct {
		name           string
		n1, n2         []*models.MachineNetwork
		expectedResult bool
	}{
		{
			name:           "Both nil",
			expectedResult: true,
		},
		{
			name:           "One nil, one empty",
			n1:             []*models.MachineNetwork{},
			expectedResult: true,
		},
		{
			name:           "Both empty",
			n1:             []*models.MachineNetwork{},
			n2:             []*models.MachineNetwork{},
			expectedResult: true,
		},
		{
			name: "Identical, ignore cluster id",
			n1: []*models.MachineNetwork{
				{
					Cidr:      "1.2.3.0/24",
					ClusterID: "id",
				},
				{
					Cidr: "5.6.7.0/24",
				},
			},
			n2: []*models.MachineNetwork{
				{
					Cidr: "5.6.7.0/24",
				},
				{
					Cidr: "1.2.3.0/24",
				},
			},
			expectedResult: true,
		},
		{
			name: "Different length",
			n1: []*models.MachineNetwork{
				{
					Cidr:      "1.2.3.0/24",
					ClusterID: "id",
				},
				{
					Cidr: "5.6.7.0/24",
				},
			},
			n2: []*models.MachineNetwork{
				{
					Cidr: "5.6.7.0/24",
				},
				{
					Cidr: "1.2.3.0/24",
				},
				{
					Cidr: "2.2.3.0/24",
				},
			},
			expectedResult: false,
		},
		{
			name: "Different contents",
			n1: []*models.MachineNetwork{
				{
					Cidr:      "1.2.3.0/24",
					ClusterID: "id",
				},
				{
					Cidr: "5.6.7.0/24",
				},
			},
			n2: []*models.MachineNetwork{
				{
					Cidr: "5.6.7.0/24",
				},
				{
					Cidr: "2.2.3.0/24",
				},
			},
			expectedResult: false,
		},
		{
			name: "Duplicate entries",
			n1: []*models.MachineNetwork{
				{
					Cidr:      "1.2.3.0/24",
					ClusterID: "id",
				},
				{
					Cidr:      "5.6.7.0/24",
					ClusterID: "id",
				},
			},
			n2: []*models.MachineNetwork{
				{
					Cidr: "1.2.3.0/24",
				},
				{
					Cidr: "1.2.3.0/24",
				},
			},
			expectedResult: false,
		},
	}
	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			Expect(AreMachineNetworksIdentical(t.n1, t.n2)).To(Equal(t.expectedResult))
			Expect(AreMachineNetworksIdentical(t.n2, t.n1)).To(Equal(t.expectedResult))
		})
	}
})

var _ = Describe("ArServiceNetworksIdentical", func() {
	tests := []struct {
		name           string
		n1, n2         []*models.ServiceNetwork
		expectedResult bool
	}{
		{
			name:           "Both nil",
			expectedResult: true,
		},
		{
			name:           "One nil, one empty",
			n1:             []*models.ServiceNetwork{},
			expectedResult: true,
		},
		{
			name:           "Both empty",
			n1:             []*models.ServiceNetwork{},
			n2:             []*models.ServiceNetwork{},
			expectedResult: true,
		},
		{
			name: "Identical, ignore cluster id",
			n1: []*models.ServiceNetwork{
				{
					Cidr:      "1.2.3.0/24",
					ClusterID: "id",
				},
				{
					Cidr: "5.6.7.0/24",
				},
			},
			n2: []*models.ServiceNetwork{
				{
					Cidr: "5.6.7.0/24",
				},
				{
					Cidr: "1.2.3.0/24",
				},
			},
			expectedResult: true,
		},
		{
			name: "Different length",
			n1: []*models.ServiceNetwork{
				{
					Cidr:      "1.2.3.0/24",
					ClusterID: "id",
				},
				{
					Cidr: "5.6.7.0/24",
				},
			},
			n2: []*models.ServiceNetwork{
				{
					Cidr: "5.6.7.0/24",
				},
				{
					Cidr: "1.2.3.0/24",
				},
				{
					Cidr: "2.2.3.0/24",
				},
			},
			expectedResult: false,
		},
		{
			name: "Different contents",
			n1: []*models.ServiceNetwork{
				{
					Cidr:      "1.2.3.0/24",
					ClusterID: "id",
				},
				{
					Cidr: "5.6.7.0/24",
				},
			},
			n2: []*models.ServiceNetwork{
				{
					Cidr: "5.6.7.0/24",
				},
				{
					Cidr: "2.2.3.0/24",
				},
			},
			expectedResult: false,
		},
	}
	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			Expect(AreServiceNetworksIdentical(t.n1, t.n2)).To(Equal(t.expectedResult))
		})
	}
})

var _ = Describe("ArClusterNetworksIdentical", func() {
	tests := []struct {
		name           string
		n1, n2         []*models.ClusterNetwork
		expectedResult bool
	}{
		{
			name:           "Both nil",
			expectedResult: true,
		},
		{
			name:           "One nil, one empty",
			n1:             []*models.ClusterNetwork{},
			expectedResult: true,
		},
		{
			name:           "Both empty",
			n1:             []*models.ClusterNetwork{},
			n2:             []*models.ClusterNetwork{},
			expectedResult: true,
		},
		{
			name: "Identical, ignore cluster id",
			n1: []*models.ClusterNetwork{
				{
					Cidr:       "1.2.3.0/24",
					HostPrefix: 4,
					ClusterID:  "id",
				},
				{
					Cidr:       "5.6.7.0/24",
					HostPrefix: 4,
				},
			},
			n2: []*models.ClusterNetwork{
				{
					Cidr:       "5.6.7.0/24",
					HostPrefix: 4,
				},
				{
					Cidr:       "1.2.3.0/24",
					HostPrefix: 4,
				},
			},
			expectedResult: true,
		},
		{
			name: "Different host prefix",
			n1: []*models.ClusterNetwork{
				{
					Cidr:       "1.2.3.0/24",
					HostPrefix: 4,
					ClusterID:  "id",
				},
				{
					Cidr:       "5.6.7.0/24",
					HostPrefix: 4,
				},
			},
			n2: []*models.ClusterNetwork{
				{
					Cidr:       "5.6.7.0/24",
					HostPrefix: 4,
				},
				{
					Cidr:       "1.2.3.0/24",
					HostPrefix: 5,
				},
			},
			expectedResult: false,
		},
		{
			name: "Different length",
			n1: []*models.ClusterNetwork{
				{
					Cidr:      "1.2.3.0/24",
					ClusterID: "id",
				},
				{
					Cidr: "5.6.7.0/24",
				},
			},
			n2: []*models.ClusterNetwork{
				{
					Cidr: "5.6.7.0/24",
				},
				{
					Cidr: "1.2.3.0/24",
				},
				{
					Cidr: "2.2.3.0/24",
				},
			},
			expectedResult: false,
		},
		{
			name: "Different contents",
			n1: []*models.ClusterNetwork{
				{
					Cidr:      "1.2.3.0/24",
					ClusterID: "id",
				},
				{
					Cidr: "5.6.7.0/24",
				},
			},
			n2: []*models.ClusterNetwork{
				{
					Cidr: "5.6.7.0/24",
				},
				{
					Cidr: "2.2.3.0/24",
				},
			},
			expectedResult: false,
		},
	}
	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			Expect(AreClusterNetworksIdentical(t.n1, t.n2)).To(Equal(t.expectedResult))
		})
	}
})

var _ = Describe("AreApiVipsIdentical", func() {
	tests := []struct {
		name           string
		n1, n2         []*models.APIVip
		expectedResult bool
	}{
		{
			name:           "Both nil",
			expectedResult: true,
		},
		{
			name:           "One nil, one empty",
			n1:             []*models.APIVip{},
			expectedResult: true,
		},
		{
			name:           "Both empty",
			n1:             []*models.APIVip{},
			n2:             []*models.APIVip{},
			expectedResult: true,
		},
		{
			name: "Identical, ignore cluster id",
			n1: []*models.APIVip{
				{
					IP:        "1.2.3.0",
					ClusterID: "id",
				},
				{
					IP: "5.6.7.0",
				},
			},
			n2: []*models.APIVip{
				{
					IP: "1.2.3.0",
				},
				{
					IP: "5.6.7.0",
				},
			},
			expectedResult: true,
		},
		{
			// In this comparison we don't care about the order of entries, we only care that a set
			// built from all the items is equal. If a consumer cares about of order of entries,
			// another comparison function should be used.
			name: "Identical in different order, ignore cluster id",
			n1: []*models.APIVip{
				{
					IP:        "1.2.3.0",
					ClusterID: "id",
				},
				{
					IP: "5.6.7.0",
				},
			},
			n2: []*models.APIVip{
				{
					IP: "5.6.7.0",
				},
				{
					IP: "1.2.3.0",
				},
			},
			expectedResult: true,
		},
		{
			name: "Different length",
			n1: []*models.APIVip{
				{
					IP:        "1.2.3.0",
					ClusterID: "id",
				},
				{
					IP: "5.6.7.0",
				},
			},
			n2: []*models.APIVip{
				{
					IP: "5.6.7.0",
				},
				{
					IP: "1.2.3.0",
				},
				{
					IP: "2.2.3.0",
				},
			},
			expectedResult: false,
		},
		{
			name: "Different contents",
			n1: []*models.APIVip{
				{
					IP:        "1.2.3.0",
					ClusterID: "id",
				},
				{
					IP: "5.6.7.0",
				},
			},
			n2: []*models.APIVip{
				{
					IP: "5.6.7.0",
				},
				{
					IP: "2.2.3.0",
				},
			},
			expectedResult: false,
		},
	}
	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			Expect(AreApiVipsIdentical(t.n1, t.n2)).To(Equal(t.expectedResult))
		})
	}
})

var _ = Describe("AreIngressVipsIdentical", func() {
	tests := []struct {
		name           string
		n1, n2         []*models.IngressVip
		expectedResult bool
	}{
		{
			name:           "Both nil",
			expectedResult: true,
		},
		{
			name:           "One nil, one empty",
			n1:             []*models.IngressVip{},
			expectedResult: true,
		},
		{
			name:           "Both empty",
			n1:             []*models.IngressVip{},
			n2:             []*models.IngressVip{},
			expectedResult: true,
		},
		{
			name: "Identical, ignore cluster id",
			n1: []*models.IngressVip{
				{
					IP:        "1.2.3.0",
					ClusterID: "id",
				},
				{
					IP: "5.6.7.0",
				},
			},
			n2: []*models.IngressVip{
				{
					IP: "1.2.3.0",
				},
				{
					IP: "5.6.7.0",
				},
			},
			expectedResult: true,
		},
		{
			// In this comparison we don't care about the order of entries, we only care that a set
			// built from all the items is equal. If a consumer cares about of order of entries,
			// another comparison function should be used.
			name: "Identical in different order, ignore cluster id",
			n1: []*models.IngressVip{
				{
					IP:        "1.2.3.0",
					ClusterID: "id",
				},
				{
					IP: "5.6.7.0",
				},
			},
			n2: []*models.IngressVip{
				{
					IP: "5.6.7.0",
				},
				{
					IP: "1.2.3.0",
				},
			},
			expectedResult: true,
		},
		{
			name: "Different length",
			n1: []*models.IngressVip{
				{
					IP:        "1.2.3.0",
					ClusterID: "id",
				},
				{
					IP: "5.6.7.0",
				},
			},
			n2: []*models.IngressVip{
				{
					IP: "5.6.7.0",
				},
				{
					IP: "1.2.3.0",
				},
				{
					IP: "2.2.3.0",
				},
			},
			expectedResult: false,
		},
		{
			name: "Different contents",
			n1: []*models.IngressVip{
				{
					IP:        "1.2.3.0",
					ClusterID: "id",
				},
				{
					IP: "5.6.7.0",
				},
			},
			n2: []*models.IngressVip{
				{
					IP: "5.6.7.0",
				},
				{
					IP: "2.2.3.0",
				},
			},
			expectedResult: false,
		},
	}
	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			Expect(AreIngressVipsIdentical(t.n1, t.n2)).To(Equal(t.expectedResult))
		})
	}
})

var _ = Describe("GetVips", func() {
	var cluster *common.Cluster
	var ApiVips []string
	var IngressVips []string
	var PrimaryApiVip string
	var PrimaryIngressVip string
	Context("for cluster with no vips", func() {
		BeforeEach(func() {
			cluster = &common.Cluster{
				Cluster: models.Cluster{
					Name:             "cluster",
					OpenshiftVersion: "4.12",
					MachineNetworks:  []*models.MachineNetwork{{Cidr: "192.168.10.0/24"}},
				},
			}
			ApiVips = GetApiVips(cluster)
			IngressVips = GetIngressVips(cluster)
			PrimaryApiVip = GetApiVipById(cluster, 0)
			PrimaryIngressVip = GetIngressVipById(cluster, 0)
		})
		It("returns empty value as API and Ingress VIPs", func() {
			Expect(len(ApiVips)).To(Equal(0))
			Expect(len(IngressVips)).To(Equal(0))
			Expect(PrimaryApiVip).To(Equal(""))
			Expect(PrimaryIngressVip).To(Equal(""))
		})
	})
	Context("for cluster with single vip", func() {
		BeforeEach(func() {
			cluster = &common.Cluster{
				Cluster: models.Cluster{
					Name:             "cluster",
					APIVips:          []*models.APIVip{{IP: "192.168.10.10"}},
					IngressVips:      []*models.IngressVip{{IP: "192.168.10.11"}},
					OpenshiftVersion: "4.12",
					MachineNetworks:  []*models.MachineNetwork{{Cidr: "192.168.10.0/24"}},
				},
			}
			ApiVips = GetApiVips(cluster)
			IngressVips = GetIngressVips(cluster)
			PrimaryApiVip = GetApiVipById(cluster, 0)
			PrimaryIngressVip = GetIngressVipById(cluster, 0)
		})
		It("returns API and Ingress VIP correctly", func() {
			Expect(len(ApiVips)).To(Equal(1))
			Expect(len(IngressVips)).To(Equal(1))
			Expect(ApiVips[0]).To(Equal("192.168.10.10"))
			Expect(IngressVips[0]).To(Equal("192.168.10.11"))
			Expect(PrimaryApiVip).To(Equal("192.168.10.10"))
			Expect(PrimaryIngressVip).To(Equal("192.168.10.11"))
		})
	})
	Context("for cluster with dual-stack vips", func() {
		BeforeEach(func() {
			cluster = &common.Cluster{
				Cluster: models.Cluster{
					Name:             "cluster",
					APIVips:          []*models.APIVip{{IP: "192.168.10.10"}, {IP: "1001:db8:0:200::78"}},
					IngressVips:      []*models.IngressVip{{IP: "192.168.10.11"}, {IP: "1001:db8:0:200::79"}},
					OpenshiftVersion: "4.12",
					MachineNetworks:  []*models.MachineNetwork{{Cidr: "192.168.10.0/24"}, {Cidr: "1001:db8:0:200::/40"}},
				},
			}
			ApiVips = GetApiVips(cluster)
			IngressVips = GetIngressVips(cluster)
			PrimaryApiVip = GetApiVipById(cluster, 0)
			PrimaryIngressVip = GetIngressVipById(cluster, 0)
		})
		It("returns API and Ingress VIP correctly", func() {
			Expect(len(ApiVips)).To(Equal(2))
			Expect(len(IngressVips)).To(Equal(2))
			Expect(ApiVips[0]).To(Equal("192.168.10.10"))
			Expect(IngressVips[0]).To(Equal("192.168.10.11"))
			Expect(ApiVips[1]).To(Equal("1001:db8:0:200::78"))
			Expect(IngressVips[1]).To(Equal("1001:db8:0:200::79"))
			Expect(PrimaryApiVip).To(Equal("192.168.10.10"))
			Expect(PrimaryIngressVip).To(Equal("192.168.10.11"))
		})
	})
})
var _ = Describe("FindInterfaceByIP", func() {
	var allInterfaces []*models.Interface
	var searchedInterface models.Interface

	BeforeEach(func() {
		searchedInterface = models.Interface{
			IPV4Addresses: []string{"192.168.1.3/24"},
			IPV6Addresses: []string{"1001:db8:0:200::78/48", "1001:db8:0:200::79/48"},
		}
		allInterfaces = []*models.Interface{
			{
				IPV4Addresses: []string{"192.168.1.1/24"},
			},
			{
				IPV6Addresses: []string{"1001:db8:0:200::80/48"},
			},
			&searchedInterface,
		}

	})
	DescribeTable("FindInterfaceByIP", func(allInterfaces *[]*models.Interface, lookForIp string, expectedInterface *models.Interface, expectedError string) {
		int, err := FindInterfaceByIPString(lookForIp, *allInterfaces)
		if expectedError == "" {
			Expect(err).To(BeNil())
			Expect(int).To(Equal(expectedInterface))
		} else {
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(Equal(expectedError))
		}
	},
		Entry("Look for IPv4", &allInterfaces, "192.168.1.3", &searchedInterface, ""),
		Entry("Look for IPv6", &allInterfaces, "1001:db8:0:200::79", &searchedInterface, ""),
		Entry("Invalid searched IP", &allInterfaces, "invalid", nil, "ParseAddr(\"invalid\"): unable to parse IP"),
		Entry("Empty list all interfaces", &[]*models.Interface{}, "192.168.1.3", nil, "Cannot find the network interface on which the IP 192.168.1.3 is set"),
		Entry("Searched IP does not match any of the interfaces", &allInterfaces, "99.99.99.99", nil, "Cannot find the network interface on which the IP 99.99.99.99 is set"),
	)
})

var _ = Describe("IsNicBelongsAnyMachineNetwork", func() {
	DescribeTable("", func(outgoingNicName string, mNetworks []*net.IPNet, interfaces []*models.Interface, isIPV6 bool, expectedResult bool) {
		isNicBelongs, err := IsNicBelongsAnyMachineNetwork(outgoingNicName, mNetworks, interfaces, isIPV6)
		Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("Debugging info: %v", err))
		Expect(isNicBelongs).To(Equal(expectedResult), fmt.Sprintf("Debugging info: actualResult: %t, expectedResult: %t", isNicBelongs, expectedResult))
	},
		Entry("nic belongs to a machine network", "eth0", mNetworks, []*models.Interface{{IPV4Addresses: []string{"192.168.10.2/24"}, Name: "eth0"}}, false, true),
		Entry("nic doesn't belongs to any machine network", "eth0", mNetworks, []*models.Interface{{IPV4Addresses: []string{"192.168.12.2/24"}, Name: "eth0"}}, false, false),
	)
})

var _ = Describe("IsIPBelongsToAnyMachineNetwork", func() {
	DescribeTable("", func(ip net.IP, mNetworks []*net.IPNet, expectedResult bool) {
		isIPBelongs := IsIPBelongsToAnyMachineNetwork(ip, mNetworks)
		Expect(isIPBelongs).To(Equal(expectedResult))
	},
		Entry("IP belongs to a machine network", net.IP{192, 168, 10, 0}, mNetworks, true),
		Entry("IP doesn't belongs to any machine network", net.IP{192, 168, 12, 0}, mNetworks, false),
	)
})

var _ = Describe("ComputeParsedMachineNetworks", func() {
	mNets := []*models.MachineNetwork{
		{Cidr: "192.168.10.0/24"},
	}
	expectedResult := []*net.IPNet{
		{
			IP:   net.IP{192, 168, 10, 0},        // Network Address
			Mask: net.IPv4Mask(255, 255, 255, 0), // Subnet Mask
		},
	}
	parsedMachineNetworks, err := ComputeParsedMachineNetworks(mNets)
	Expect(err).ShouldNot(HaveOccurred(), fmt.Sprintf("Debugging info: %v", err))
	Expect(parsedMachineNetworks).To(Equal(expectedResult))

})

var _ = Describe("NormalizeCIDR", func() {
	It("Normalizes a CIDR", func() {
		cidr := "2A00:8A00:4000:0d80::/64"
		expected := "2a00:8a00:4000:d80::/64"
		actual, err := NormalizeCIDR(cidr)
		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(expected))
	})
	It("Normalizes a CIDR - no change", func() {
		cidr := "192.168.1.0/24"
		expected := "192.168.1.0/24"
		actual, err := NormalizeCIDR(cidr)
		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(expected))
	})
	It("Empty CIDR", func() {
		cidr := ""
		expected := ""
		actual, err := NormalizeCIDR(cidr)
		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(expected))
	})
	It("Invalid CIDR", func() {
		cidr := "invalid"
		_, err := NormalizeCIDR(cidr)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("Network Comparison Functions with Dual-Stack Support", func() {

	Describe("AreMachineNetworksIdentical", func() {
		Context("Single-stack networks", func() {
			It("should return true for identical IPv4 networks in any order", func() {
				n1 := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
					{Cidr: "192.168.1.0/24"},
				}
				n2 := []*models.MachineNetwork{
					{Cidr: "192.168.1.0/24"},
					{Cidr: "10.0.0.0/16"},
				}
				Expect(AreMachineNetworksIdentical(n1, n2)).To(BeTrue())
			})

			It("should return false for different single-stack networks", func() {
				n1 := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
				}
				n2 := []*models.MachineNetwork{
					{Cidr: "192.168.1.0/24"},
				}
				Expect(AreMachineNetworksIdentical(n1, n2)).To(BeFalse())
			})
		})

		Context("Dual-stack networks", func() {
			It("should return true for identical dual-stack networks in same order", func() {
				n1 := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
					{Cidr: "2001:db8::/64"},
				}
				n2 := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
					{Cidr: "2001:db8::/64"},
				}
				Expect(AreMachineNetworksIdentical(n1, n2)).To(BeTrue())
			})

			It("should return false for identical dual-stack networks in different order", func() {
				n1 := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
					{Cidr: "2001:db8::/64"},
				}
				n2 := []*models.MachineNetwork{
					{Cidr: "2001:db8::/64"},
					{Cidr: "10.0.0.0/16"},
				}
				Expect(AreMachineNetworksIdentical(n1, n2)).To(BeFalse())
			})

			It("should return false for different dual-stack networks", func() {
				n1 := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
					{Cidr: "2001:db8::/64"},
				}
				n2 := []*models.MachineNetwork{
					{Cidr: "192.168.1.0/24"},
					{Cidr: "2001:db8:1::/64"},
				}
				Expect(AreMachineNetworksIdentical(n1, n2)).To(BeFalse())
			})
		})
	})

	Describe("AreServiceNetworksIdentical", func() {
		Context("Single-stack networks", func() {
			It("should return true for identical IPv4 networks in any order", func() {
				n1 := []*models.ServiceNetwork{
					{Cidr: "172.30.0.0/16"},
					{Cidr: "172.31.0.0/16"},
				}
				n2 := []*models.ServiceNetwork{
					{Cidr: "172.31.0.0/16"},
					{Cidr: "172.30.0.0/16"},
				}
				Expect(AreServiceNetworksIdentical(n1, n2)).To(BeTrue())
			})
		})

		Context("Dual-stack networks", func() {
			It("should return true for identical dual-stack networks in same order", func() {
				n1 := []*models.ServiceNetwork{
					{Cidr: "172.30.0.0/16"},
					{Cidr: "2001:db8:1::/64"},
				}
				n2 := []*models.ServiceNetwork{
					{Cidr: "172.30.0.0/16"},
					{Cidr: "2001:db8:1::/64"},
				}
				Expect(AreServiceNetworksIdentical(n1, n2)).To(BeTrue())
			})

			It("should return false for identical dual-stack networks in different order", func() {
				n1 := []*models.ServiceNetwork{
					{Cidr: "172.30.0.0/16"},
					{Cidr: "2001:db8:1::/64"},
				}
				n2 := []*models.ServiceNetwork{
					{Cidr: "2001:db8:1::/64"},
					{Cidr: "172.30.0.0/16"},
				}
				Expect(AreServiceNetworksIdentical(n1, n2)).To(BeFalse())
			})
		})
	})

	Describe("AreClusterNetworksIdentical", func() {
		Context("Single-stack networks", func() {
			It("should return true for identical IPv4 networks in any order", func() {
				n1 := []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
					{Cidr: "10.132.0.0/14", HostPrefix: 24},
				}
				n2 := []*models.ClusterNetwork{
					{Cidr: "10.132.0.0/14", HostPrefix: 24},
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
				}
				Expect(AreClusterNetworksIdentical(n1, n2)).To(BeTrue())
			})
		})

		Context("Dual-stack networks", func() {
			It("should return true for identical dual-stack networks in same order", func() {
				n1 := []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
					{Cidr: "2001:db8:2::/64", HostPrefix: 64},
				}
				n2 := []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
					{Cidr: "2001:db8:2::/64", HostPrefix: 64},
				}
				Expect(AreClusterNetworksIdentical(n1, n2)).To(BeTrue())
			})

			It("should return false for identical dual-stack networks in different order", func() {
				n1 := []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
					{Cidr: "2001:db8:2::/64", HostPrefix: 64},
				}
				n2 := []*models.ClusterNetwork{
					{Cidr: "2001:db8:2::/64", HostPrefix: 64},
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
				}
				Expect(AreClusterNetworksIdentical(n1, n2)).To(BeFalse())
			})

			It("should return false for networks with different host prefixes", func() {
				n1 := []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
					{Cidr: "2001:db8:2::/64", HostPrefix: 64},
				}
				n2 := []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 24},
					{Cidr: "2001:db8:2::/64", HostPrefix: 64},
				}
				Expect(AreClusterNetworksIdentical(n1, n2)).To(BeFalse())
			})
		})
	})

	Describe("AreApiVipsIdentical", func() {
		Context("Single-stack VIPs", func() {
			It("should return true for identical IPv4 VIPs in any order", func() {
				n1 := []*models.APIVip{
					{IP: "10.0.1.1"},
					{IP: "10.0.1.2"},
				}
				n2 := []*models.APIVip{
					{IP: "10.0.1.2"},
					{IP: "10.0.1.1"},
				}
				Expect(AreApiVipsIdentical(n1, n2)).To(BeTrue())
			})
		})

		Context("Dual-stack VIPs", func() {
			It("should return true for identical dual-stack VIPs in same order", func() {
				n1 := []*models.APIVip{
					{IP: "10.0.1.1"},
					{IP: "2001:db8::1"},
				}
				n2 := []*models.APIVip{
					{IP: "10.0.1.1"},
					{IP: "2001:db8::1"},
				}
				Expect(AreApiVipsIdentical(n1, n2)).To(BeTrue())
			})

			It("should return false for identical dual-stack VIPs in different order", func() {
				n1 := []*models.APIVip{
					{IP: "10.0.1.1"},
					{IP: "2001:db8::1"},
				}
				n2 := []*models.APIVip{
					{IP: "2001:db8::1"},
					{IP: "10.0.1.1"},
				}
				Expect(AreApiVipsIdentical(n1, n2)).To(BeFalse())
			})
		})
	})

	Describe("AreIngressVipsIdentical", func() {
		Context("Single-stack VIPs", func() {
			It("should return true for identical IPv4 VIPs in any order", func() {
				n1 := []*models.IngressVip{
					{IP: "10.0.1.3"},
					{IP: "10.0.1.4"},
				}
				n2 := []*models.IngressVip{
					{IP: "10.0.1.4"},
					{IP: "10.0.1.3"},
				}
				Expect(AreIngressVipsIdentical(n1, n2)).To(BeTrue())
			})
		})

		Context("Dual-stack VIPs", func() {
			It("should return true for identical dual-stack VIPs in same order", func() {
				n1 := []*models.IngressVip{
					{IP: "10.0.1.3"},
					{IP: "2001:db8::2"},
				}
				n2 := []*models.IngressVip{
					{IP: "10.0.1.3"},
					{IP: "2001:db8::2"},
				}
				Expect(AreIngressVipsIdentical(n1, n2)).To(BeTrue())
			})

			It("should return false for identical dual-stack VIPs in different order", func() {
				n1 := []*models.IngressVip{
					{IP: "10.0.1.3"},
					{IP: "2001:db8::2"},
				}
				n2 := []*models.IngressVip{
					{IP: "2001:db8::2"},
					{IP: "10.0.1.3"},
				}
				Expect(AreIngressVipsIdentical(n1, n2)).To(BeFalse())
			})
		})
	})

	Describe("isDualStackItems helper function", func() {
		Context("Machine Networks", func() {
			It("should return true for dual-stack machine networks", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
					{Cidr: "2001:db8::/64"},
				}
				Expect(isDualStackItems(networks)).To(BeTrue())
			})

			It("should return false for single-stack IPv4 machine networks", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
					{Cidr: "192.168.1.0/24"},
				}
				Expect(isDualStackItems(networks)).To(BeFalse())
			})

			It("should return false for single-stack IPv6 machine networks", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "2001:db8::/64"},
					{Cidr: "2001:db8:1::/64"},
				}
				Expect(isDualStackItems(networks)).To(BeFalse())
			})
		})

		Context("Service Networks", func() {
			It("should return true for dual-stack service networks", func() {
				networks := []*models.ServiceNetwork{
					{Cidr: "172.30.0.0/16"},
					{Cidr: "2001:db8:1::/64"},
				}
				Expect(isDualStackItems(networks)).To(BeTrue())
			})
		})

		Context("Cluster Networks", func() {
			It("should return true for dual-stack cluster networks", func() {
				networks := []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14"},
					{Cidr: "2001:db8:2::/64"},
				}
				Expect(isDualStackItems(networks)).To(BeTrue())
			})
		})

		Context("API VIPs", func() {
			It("should return true for dual-stack API VIPs", func() {
				vips := []*models.APIVip{
					{IP: "10.0.1.1"},
					{IP: "2001:db8::1"},
				}
				Expect(isDualStackItems(vips)).To(BeTrue())
			})
		})

		Context("Ingress VIPs", func() {
			It("should return true for dual-stack Ingress VIPs", func() {
				vips := []*models.IngressVip{
					{IP: "10.0.1.2"},
					{IP: "2001:db8::2"},
				}
				Expect(isDualStackItems(vips)).To(BeTrue())
			})
		})

		Context("Edge cases", func() {
			It("should return false for unknown types", func() {
				Expect(isDualStackItems("unknown")).To(BeFalse())
			})

			It("should return false for empty slices", func() {
				var networks []*models.MachineNetwork
				Expect(isDualStackItems(networks)).To(BeFalse())
			})

			It("should return false for single item slices", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
				}
				Expect(isDualStackItems(networks)).To(BeFalse())
			})
		})
	})
})
