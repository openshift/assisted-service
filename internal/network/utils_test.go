package network

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

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

var _ = Describe("Is global CIDR", func() {
	DescribeTable(
		"Returns the expected result",
		func(address string, expected bool) {
			actual, err := IsGlobalCIDR(address)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		},
		Entry("IPv4 loopback", "127.0.0.1/32", false),
		Entry("IPv4 global", "192.168.0.1/24", true),
		Entry("IPv6 loopback", "::1/128", false),
		Entry("IPv6 global", "5dc8:725d:26ae:1192:d336:54a3:d7c7:23a7/64", true),
	)

	It("Returns error if address can't be parsed", func() {
		result, err := IsGlobalCIDR("junk")
		Expect(err).To(HaveOccurred())
		message := err.Error()
		Expect(message).To(ContainSubstring("junk"))
		Expect(result).To(BeFalse())
	})
})
