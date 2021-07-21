package network

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	models "github.com/openshift/assisted-service/models/v1"
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
