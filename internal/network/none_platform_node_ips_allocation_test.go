package network

import (
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

var _ = Describe("none platform node ips allocation", func() {
	createInterface := func(name string, addresses ...string) *models.Interface {
		var ipv4Addresses, ipv6Addresses []string
		for _, a := range addresses {
			if IsIPV4CIDR(a) {
				ipv4Addresses = append(ipv4Addresses, a)
			} else {
				ipv6Addresses = append(ipv6Addresses, a)
			}
		}
		return &models.Interface{
			IPV4Addresses: ipv4Addresses,
			IPV6Addresses: ipv6Addresses,
			Name:          name,
		}
	}

	createInterfaces := func(interfaces ...*models.Interface) []*models.Interface {
		return interfaces
	}
	createRoute := func(intf, dest, gw string, family, metric int32) *models.Route {
		return &models.Route{
			Destination: dest,
			Family:      family,
			Interface:   intf,
			Metric:      metric,
			Gateway:     gw,
		}
	}
	createDefaultRoute := func(intf string, family int32) *models.Route {
		var dest, gw string
		if family == unix.AF_INET {
			dest = "0.0.0.0"
			gw = "1.1.1.1"
		} else {
			dest = "::"
			gw = "1:1:1::1"
		}
		return createRoute(
			intf,
			dest,
			gw,
			family,
			100)
	}
	createRoutes := func(routes ...*models.Route) []*models.Route {
		return routes
	}
	createInventory := func(interfaces []*models.Interface, routes []*models.Route) string {
		inventory := models.Inventory{
			Interfaces: interfaces,
			Routes:     routes,
		}
		b, err := json.Marshal(&inventory)
		Expect(err).ToNot(HaveOccurred())
		return string(b)
	}
	createHost := func(inventory string, role models.HostRole) *models.Host {
		id := strfmt.UUID(uuid.New().String())
		return &models.Host{
			ID:            &id,
			Inventory:     inventory,
			SuggestedRole: role,
			Role:          role,
		}
	}
	generateL3ConnectedAddresses := func(connectedAddresses map[strfmt.UUID][]string) string {
		majorityGroups := &Connectivity{
			L3ConnectedAddresses: connectedAddresses,
		}
		tmp, err := json.Marshal(majorityGroups)
		Expect(err).ToNot(HaveOccurred())
		return string(tmp)
	}
	createCluster := func(hosts []*models.Host, connectivity string, networking common.TestNetworking) *common.Cluster {
		id := strfmt.UUID(uuid.New().String())
		return &common.Cluster{
			Cluster: models.Cluster{
				ID:                         &id,
				Hosts:                      hosts,
				ConnectivityMajorityGroups: connectivity,
				ClusterNetworks:            networking.ClusterNetworks,
				ServiceNetworks:            networking.ServiceNetworks,
			},
		}
	}
	It("no hosts, no connectivity", func() {
		cluster := createCluster(nil, "", common.TestIPv4Networking)
		allocation, err := GenerateNonePlatformAddressAllocation(cluster, logrus.New())
		Expect(err).To(HaveOccurred())
		Expect(allocation).To(BeNil())
	})
	It("only masters - one network", func() {
		h1 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.4/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h2 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.5/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h3 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.6/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		hosts := []*models.Host{
			h1,
			h2,
			h3,
		}
		connectivity := map[strfmt.UUID][]string{
			*h1.ID: {"1.2.3.4"},
			*h2.ID: {"1.2.3.5"},
			*h3.ID: {"1.2.3.6"},
		}
		cluster := createCluster(hosts, generateL3ConnectedAddresses(connectivity), common.TestIPv4Networking)
		allocation, err := GenerateNonePlatformAddressAllocation(cluster, logrus.New())
		Expect(err).ToNot(HaveOccurred())
		Expect(allocation).To(Equal(map[strfmt.UUID]*NodeIpAllocation{
			*h1.ID: {
				NodeIp: "1.2.3.4",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h2.ID: {
				NodeIp: "1.2.3.5",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h3.ID: {
				NodeIp: "1.2.3.6",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
		}))
	})

	It("only masters - one network, IPv6", func() {
		h1 := createHost(createInventory(createInterfaces(createInterface("eth0", "1001:db8::1/120")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET6))), models.HostRoleMaster)
		h2 := createHost(createInventory(createInterfaces(createInterface("eth0", "1001:db8::2/120")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET6))), models.HostRoleMaster)
		h3 := createHost(createInventory(createInterfaces(createInterface("eth0", "1001:db8::3/120")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET6))), models.HostRoleMaster)
		hosts := []*models.Host{
			h1,
			h2,
			h3,
		}
		connectivity := map[strfmt.UUID][]string{
			*h1.ID: {"1001:db8::1"},
			*h2.ID: {"1001:db8::2"},
			*h3.ID: {"1001:db8::3"},
		}
		cluster := createCluster(hosts, generateL3ConnectedAddresses(connectivity), common.TestIPv6Networking)
		allocation, err := GenerateNonePlatformAddressAllocation(cluster, logrus.New())
		Expect(err).ToNot(HaveOccurred())
		Expect(allocation).To(Equal(map[strfmt.UUID]*NodeIpAllocation{
			*h1.ID: {
				NodeIp: "1001:db8::1",
				HintIp: "1001:db8::",
				Cidr:   "1001:db8::/120",
			},
			*h2.ID: {
				NodeIp: "1001:db8::2",
				HintIp: "1001:db8::",
				Cidr:   "1001:db8::/120",
			},
			*h3.ID: {
				NodeIp: "1001:db8::3",
				HintIp: "1001:db8::",
				Cidr:   "1001:db8::/120",
			},
		}))
	})

	It("only masters - two network", func() {
		h1 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.4/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h2 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.5/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h3 := createHost(createInventory(createInterfaces(createInterface("eth0", "5.6.7.8/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		hosts := []*models.Host{
			h1,
			h2,
			h3,
		}
		connectivity := map[strfmt.UUID][]string{
			*h1.ID: {"1.2.3.4"},
			*h2.ID: {"1.2.3.5"},
			*h3.ID: {"5.6.7.8"},
		}
		cluster := createCluster(hosts, generateL3ConnectedAddresses(connectivity), common.TestIPv4Networking)
		allocation, err := GenerateNonePlatformAddressAllocation(cluster, logrus.New())
		Expect(err).ToNot(HaveOccurred())
		Expect(allocation).To(Equal(map[strfmt.UUID]*NodeIpAllocation{
			*h1.ID: {
				NodeIp: "1.2.3.4",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h2.ID: {
				NodeIp: "1.2.3.5",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h3.ID: {
				NodeIp: "5.6.7.8",
				HintIp: "5.6.7.0",
				Cidr:   "5.6.7.0/24",
			},
		}))
	})
	It("only masters - two network, with overlapping networks", func() {
		h1 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.4/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h2 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.5/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h3 := createHost(createInventory(createInterfaces(createInterface("eth1", "1.2.3.6/24"), createInterface("eth0", "5.6.7.8/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET), createDefaultRoute("eth1", unix.AF_INET))), models.HostRoleMaster)
		hosts := []*models.Host{
			h1,
			h2,
			h3,
		}
		connectivity := map[strfmt.UUID][]string{
			*h1.ID: {"1.2.3.4"},
			*h2.ID: {"1.2.3.5"},
			*h3.ID: {"5.6.7.8", "1.2.3.6"},
		}
		cluster := createCluster(hosts, generateL3ConnectedAddresses(connectivity), common.TestIPv4Networking)
		allocation, err := GenerateNonePlatformAddressAllocation(cluster, logrus.New())
		Expect(err).ToNot(HaveOccurred())
		Expect(allocation).To(Equal(map[strfmt.UUID]*NodeIpAllocation{
			*h1.ID: {
				NodeIp: "1.2.3.4",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h2.ID: {
				NodeIp: "1.2.3.5",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h3.ID: {
				NodeIp: "1.2.3.6",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
		}))
	})
	It("only masters - two network, with overlapping networks.  node-ip not connected", func() {
		h1 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.4/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h2 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.5/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h3 := createHost(createInventory(createInterfaces(createInterface("eth1", "1.2.3.6/24"), createInterface("eth0", "5.6.7.8/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET), createDefaultRoute("eth1", unix.AF_INET))), models.HostRoleMaster)
		hosts := []*models.Host{
			h1,
			h2,
			h3,
		}
		connectivity := map[strfmt.UUID][]string{
			*h1.ID: {"1.2.3.4"},
			*h2.ID: {"1.2.3.5"},
			*h3.ID: {"5.6.7.8"},
		}
		cluster := createCluster(hosts, generateL3ConnectedAddresses(connectivity), common.TestIPv4Networking)
		allocation, err := GenerateNonePlatformAddressAllocation(cluster, logrus.New())
		Expect(err).To(HaveOccurred())
		Expect(allocation).To(BeNil())
	})
	It("only masters - two network, with overlapping networks, with colliding networks", func() {
		h1 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.4/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h2 := createHost(createInventory(createInterfaces(createInterface("eth0", "5.6.7.7/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h3 := createHost(createInventory(createInterfaces(createInterface("eth1", "1.2.3.6/24"), createInterface("eth0", "5.6.7.8/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET), createDefaultRoute("eth1", unix.AF_INET))), models.HostRoleMaster)
		hosts := []*models.Host{
			h1,
			h2,
			h3,
		}
		connectivity := map[strfmt.UUID][]string{
			*h1.ID: {"1.2.3.4"},
			*h2.ID: {"5.6.7.7"},
			*h3.ID: {"5.6.7.8", "1.2.3.6"},
		}
		cluster := createCluster(hosts, generateL3ConnectedAddresses(connectivity), common.TestIPv4Networking)
		allocation, err := GenerateNonePlatformAddressAllocation(cluster, logrus.New())
		Expect(err).To(HaveOccurred())
		Expect(allocation).To(BeNil())
	})
	It("only masters - three network, with overlapping networks", func() {
		h1 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.4/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h2 := createHost(createInventory(createInterfaces(createInterface("eth0", "5.6.7.7/24"), createInterface("eth1", "6.7.8.9/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET), createDefaultRoute("eth1", unix.AF_INET))), models.HostRoleMaster)
		h3 := createHost(createInventory(createInterfaces(createInterface("eth1", "1.2.3.6/24"), createInterface("eth0", "5.6.7.8/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET), createDefaultRoute("eth1", unix.AF_INET))), models.HostRoleMaster)
		hosts := []*models.Host{
			h1,
			h2,
			h3,
		}
		connectivity := map[strfmt.UUID][]string{
			*h1.ID: {"1.2.3.4"},
			*h2.ID: {"5.6.7.7", "6.7.8.9"},
			*h3.ID: {"5.6.7.8", "1.2.3.6"},
		}
		cluster := createCluster(hosts, generateL3ConnectedAddresses(connectivity), common.TestIPv4Networking)
		allocation, err := GenerateNonePlatformAddressAllocation(cluster, logrus.New())
		Expect(err).ToNot(HaveOccurred())
		Expect(allocation).To(Equal(map[strfmt.UUID]*NodeIpAllocation{
			*h1.ID: {
				NodeIp: "1.2.3.4",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h2.ID: {
				NodeIp: "6.7.8.9",
				HintIp: "6.7.8.0",
				Cidr:   "6.7.8.0/24",
			},
			*h3.ID: {
				NodeIp: "1.2.3.6",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
		}))
	})
	It("only masters and workers - one network", func() {
		h1 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.4/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h2 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.5/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h3 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.6/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h4 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.7/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleWorker)
		h5 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.8/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleWorker)
		hosts := []*models.Host{
			h1,
			h2,
			h3,
			h4,
			h5,
		}
		connectivity := map[strfmt.UUID][]string{
			*h1.ID: {"1.2.3.4"},
			*h2.ID: {"1.2.3.5"},
			*h3.ID: {"1.2.3.6"},
			*h4.ID: {"1.2.3.7"},
			*h5.ID: {"1.2.3.8"},
		}
		cluster := createCluster(hosts, generateL3ConnectedAddresses(connectivity), common.TestIPv4Networking)
		allocation, err := GenerateNonePlatformAddressAllocation(cluster, logrus.New())
		Expect(err).ToNot(HaveOccurred())
		Expect(allocation).To(Equal(map[strfmt.UUID]*NodeIpAllocation{
			*h1.ID: {
				NodeIp: "1.2.3.4",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h2.ID: {
				NodeIp: "1.2.3.5",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h3.ID: {
				NodeIp: "1.2.3.6",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h4.ID: {
				NodeIp: "1.2.3.7",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h5.ID: {
				NodeIp: "1.2.3.8",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
		}))
	})
	It("masters and workers - three network, with overlapping networks - with collisions", func() {
		h1 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.4/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h2 := createHost(createInventory(createInterfaces(createInterface("eth0", "5.6.7.7/24"), createInterface("eth1", "6.7.8.9/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET), createDefaultRoute("eth1", unix.AF_INET))), models.HostRoleMaster)
		h3 := createHost(createInventory(createInterfaces(createInterface("eth1", "1.2.3.6/24"), createInterface("eth0", "5.6.7.8/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET), createDefaultRoute("eth1", unix.AF_INET))), models.HostRoleMaster)
		h4 := createHost(createInventory(createInterfaces(createInterface("eth0", "5.6.7.9/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleWorker)
		h5 := createHost(createInventory(createInterfaces(createInterface("eth0", "5.6.7.10/24"), createInterface("eth1", "1.2.3.8/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET), createDefaultRoute("eth1", unix.AF_INET))), models.HostRoleWorker)
		hosts := []*models.Host{
			h1,
			h2,
			h3,
			h4,
			h5,
		}
		connectivity := map[strfmt.UUID][]string{
			*h1.ID: {"1.2.3.4"},
			*h2.ID: {"5.6.7.7", "6.7.8.9"},
			*h3.ID: {"5.6.7.8", "1.2.3.6"},
			*h4.ID: {"5.6.7.9"},
			*h5.ID: {"5.6.7.10", "1.2.3.8"},
		}
		cluster := createCluster(hosts, generateL3ConnectedAddresses(connectivity), common.TestIPv4Networking)
		allocation, err := GenerateNonePlatformAddressAllocation(cluster, logrus.New())
		Expect(err).To(HaveOccurred())
		Expect(allocation).To(BeNil())
	})
	It("masters and workers - three network, with overlapping networks - no collisions", func() {
		h1 := createHost(createInventory(createInterfaces(createInterface("eth0", "1.2.3.4/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleMaster)
		h2 := createHost(createInventory(createInterfaces(createInterface("eth0", "5.6.7.7/24"), createInterface("eth1", "6.7.8.9/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET), createDefaultRoute("eth1", unix.AF_INET))), models.HostRoleMaster)
		h3 := createHost(createInventory(createInterfaces(createInterface("eth1", "1.2.3.6/24"), createInterface("eth0", "5.6.7.8/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET), createDefaultRoute("eth1", unix.AF_INET))), models.HostRoleMaster)
		h4 := createHost(createInventory(createInterfaces(createInterface("eth0", "6.7.8.10/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET))), models.HostRoleWorker)
		h5 := createHost(createInventory(createInterfaces(createInterface("eth0", "5.6.7.10/24"), createInterface("eth1", "1.2.3.8/24")),
			createRoutes(createDefaultRoute("eth0", unix.AF_INET), createDefaultRoute("eth1", unix.AF_INET))), models.HostRoleWorker)
		hosts := []*models.Host{
			h1,
			h2,
			h3,
			h4,
			h5,
		}
		connectivity := map[strfmt.UUID][]string{
			*h1.ID: {"1.2.3.4"},
			*h2.ID: {"5.6.7.7", "6.7.8.9"},
			*h3.ID: {"5.6.7.8", "1.2.3.6"},
			*h4.ID: {"6.7.8.10"},
			*h5.ID: {"5.6.7.10", "1.2.3.8"},
		}
		cluster := createCluster(hosts, generateL3ConnectedAddresses(connectivity), common.TestIPv4Networking)
		allocation, err := GenerateNonePlatformAddressAllocation(cluster, logrus.New())
		Expect(err).ToNot(HaveOccurred())
		Expect(allocation).To(Equal(map[strfmt.UUID]*NodeIpAllocation{
			*h1.ID: {
				NodeIp: "1.2.3.4",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h2.ID: {
				NodeIp: "6.7.8.9",
				HintIp: "6.7.8.0",
				Cidr:   "6.7.8.0/24",
			},
			*h3.ID: {
				NodeIp: "1.2.3.6",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
			*h4.ID: {
				NodeIp: "6.7.8.10",
				HintIp: "6.7.8.0",
				Cidr:   "6.7.8.0/24",
			},
			*h5.ID: {
				NodeIp: "1.2.3.8",
				HintIp: "1.2.3.0",
				Cidr:   "1.2.3.0/24",
			},
		}))
	})
})
