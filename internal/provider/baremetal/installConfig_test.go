package baremetal

import (
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("AddPlatformToInstallConfig", func() {
	var (
		logger    logrus.FieldLogger
		cluster   *common.Cluster
		infraEnvs []*common.InfraEnv
		cfg       *installcfg.InstallerConfigBaremetal
		provider  provider.Provider
	)

	BeforeEach(func() {
		logger = common.GetTestLog()
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				OpenshiftVersion: "4.18",
			},
		}
		infraEnvs = []*common.InfraEnv{{
			InfraEnv: models.InfraEnv{},
		}}
		cfg = &installcfg.InstallerConfigBaremetal{
			ControlPlane: struct {
				Hyperthreading string "json:\"hyperthreading,omitempty\""
				Name           string "json:\"name\""
				Replicas       int    "json:\"replicas\""
			}{
				Replicas: 1,
			},
			Compute: []struct {
				Hyperthreading string "json:\"hyperthreading,omitempty\""
				Name           string "json:\"name\""
				Replicas       int    "json:\"replicas\""
			}{{
				Replicas: 1,
			}},
		}
		provider = NewBaremetalProvider(logger)
	})

	Context("Add NTP sources", func() {
		It("Does nothing if there are no NTP sources", func() {
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(BeEmpty())
		})

		It("Adds one NTP source from cluster", func() {
			cluster.AdditionalNtpSource = "1.1.1.1"
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1"))
		})

		It("Adds multiple NTP sources from cluster", func() {
			cluster.AdditionalNtpSource = "1.1.1.1,2.2.2.2,3.3.3.3"
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1", "2.2.2.2", "3.3.3.3"))
		})

		It("Removes extra white space in NTP sources from cluster", func() {
			cluster.AdditionalNtpSource = "  1.1.1.1,   \t  2.2.2.2 , 3.3.3.3  "
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1", "2.2.2.2", "3.3.3.3"))
		})

		It("Adds one source from one infrastructure environment", func() {
			infraEnvs[0].AdditionalNtpSources = "1.1.1.1"
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1"))
		})

		It("Adds multiple sources from one infrastructure environment", func() {
			infraEnvs[0].AdditionalNtpSources = "1.1.1.1,2.2.2.2,3.3.3.3"
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1", "2.2.2.2", "3.3.3.3"))
		})

		It("Removes extra white space in NTP sources from infrastructure environment", func() {
			infraEnvs[0].AdditionalNtpSources = "  1.1.1.1,   \t  2.2.2.2 , 3.3.3.3  "
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1", "2.2.2.2", "3.3.3.3"))
		})

		It("Ignores sources from infrastructure environment if there are NTP sources in the cluster", func() {
			cluster.AdditionalNtpSource = "1.1.1.1"
			infraEnvs[0].AdditionalNtpSources = "2.2.2.2"
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1"))
		})

		It("Combines NTP sources from multiple infrastructure environments", func() {
			newInfraEnvs := []*common.InfraEnv{
				{
					InfraEnv: models.InfraEnv{
						AdditionalNtpSources: "1.1.1.1",
					},
				},
				{
					InfraEnv: models.InfraEnv{
						AdditionalNtpSources: "2.2.2.2",
					},
				},
				{
					InfraEnv: models.InfraEnv{
						AdditionalNtpSources: "3.3.3.3",
					},
				},
			}
			err := provider.AddPlatformToInstallConfig(cfg, cluster, newInfraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1", "2.2.2.2", "3.3.3.3"))
		})
	})

	Context("addLoadBalancer", func() {
		It("Does nothing if there is no load balancer", func() {
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.LoadBalancer).To(BeNil())
		})

		It("Does nothing if there if the load balancer is cluster-managed", func() {
			cluster.LoadBalancer = &models.LoadBalancer{Type: models.LoadBalancerTypeClusterManaged}
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.LoadBalancer).To(BeNil())
		})

		It("Does nothing if there if the load balancer is cluster-managed", func() {
			cluster.LoadBalancer = &models.LoadBalancer{Type: models.LoadBalancerTypeClusterManaged}
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.LoadBalancer).To(BeNil())
		})

		It("Adds user-managed load balancer", func() {
			cluster.LoadBalancer = &models.LoadBalancer{Type: models.LoadBalancerTypeUserManaged}
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Baremetal.LoadBalancer).ToNot(BeNil())
			Expect(cfg.Platform.Baremetal.LoadBalancer.Type).To(Equal(configv1.LoadBalancerTypeUserManaged))
		})

		It("Returns error if load balancer type is not supported", func() {
			cluster.LoadBalancer = &models.LoadBalancer{Type: "unsupported"}
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("load balancer type is set to unsupported value 'unsupported'"))
		})
	})

	Context("getHostMachineNetworkInterfaceMACAddress", func() {
		var (
			hostID = strfmt.UUID(uuid.New().String())
		)

		It("returns an error when no machine networks found", func() {
			cluster.Hosts = []*models.Host{
				{
					ID:        &hostID,
					Inventory: common.GenerateTestInventory(),
				},
			}

			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Failed to find machine networks for baremetal cluster"))
		})

		It("returns an error when no host interface IP is contained within any machine network", func() {
			cluster.Hosts = []*models.Host{
				{
					ID:        &hostID,
					Inventory: common.GenerateTestInventory(),
				},
			}

			cluster.MachineNetworks = []*models.MachineNetwork{
				{Cidr: "1.2.4.0/24"},
				{Cidr: "1.2.2.0/24"},
			}

			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Failed to find a network interface matching any machine network"))
		})

		It("returns the correct MAC addresses", func() {
			cluster.Hosts = []*models.Host{
				{
					ID: &hostID,
					Inventory: common.GenerateTestInventoryWithMutate(
						func(i *models.Inventory) {
							i.Interfaces = []*models.Interface{
								{
									Name: "eth0",
									IPV4Addresses: []string{
										"1.2.3.4/24",
										"1.2.4.4/24",
									},
									IPV6Addresses: []string{
										"1001:db8::10/120",
									},
									MacAddress: "AA:BB:CC:DD:EE:FF",
								},
								{
									Name: "eth1",
									IPV4Addresses: []string{
										"1.2.5.4/24",
										"1.2.6.4/24",
									},
									IPV6Addresses: []string{
										"1001:db8::10/120",
									},
									MacAddress: "11:22:33:44:55:66",
								},
							}
						},
					),
				},
				{
					ID: &hostID,
					Inventory: common.GenerateTestInventoryWithMutate(
						func(i *models.Inventory) {
							i.Interfaces = []*models.Interface{
								{
									Name: "eth0",
									IPV4Addresses: []string{
										"1.2.7.4/24",
										"1.2.8.4/24",
									},
									IPV6Addresses: []string{
										"1001:db8::10/120",
									},
									MacAddress: "AA:BB:CC:DD:EE:FF",
								},
								{
									Name: "eth1",
									IPV4Addresses: []string{
										"1.2.9.4/24",
										"1.2.10.4/24",
									},
									IPV6Addresses: []string{
										"1001:db8::10/120",
									},
									MacAddress: "11:22:33:44:55:66",
								},
							}
						},
					),
				},
				{
					ID: &hostID,
					Inventory: common.GenerateTestInventoryWithMutate(
						func(i *models.Inventory) {
							i.Interfaces = []*models.Interface{
								{
									Name: "eth0",
									IPV4Addresses: []string{
										"1.2.11.4/24",
										"1.2.12.4/24",
									},
									IPV6Addresses: []string{
										"1001:db8::10/120",
									},
									MacAddress: "AA:BB:CC:DD:EE:FF",
								},
								{
									Name: "eth1",
									IPV4Addresses: []string{
										"1.2.13.4/24",
										"1.2.14.4/24",
									},
									IPV6Addresses: []string{
										"1001:db8::10/120",
									},
									MacAddress: "11:22:33:44:55:66",
								},
							}
						},
					),
				},
			}

			cluster.MachineNetworks = []*models.MachineNetwork{
				{Cidr: "1.2.3.0/24"},
				{Cidr: "1.2.8.0/24"},
				{Cidr: "1.2.14.0/24"},
			}

			cfg.ControlPlane.Replicas = 2

			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())

			Expect(cfg.Platform.Baremetal.Hosts).To(HaveLen(3))
			Expect(cfg.Platform.Baremetal.Hosts[0].BootMACAddress).To(Equal("AA:BB:CC:DD:EE:FF"))
			Expect(cfg.Platform.Baremetal.Hosts[1].BootMACAddress).To(Equal("AA:BB:CC:DD:EE:FF"))
			Expect(cfg.Platform.Baremetal.Hosts[2].BootMACAddress).To(Equal("11:22:33:44:55:66"))
		})
	})
})
