package registry

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider/vsphere"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	"gopkg.in/yaml.v2"
)

var (
	providerRegistry ProviderRegistry
	ctrl             *gomock.Controller
)

const invalidInventory = "{\"system_vendor\": \"invalid\"}"

const expectedNutanixInstallConfig411 = `apiVIPs:
- 192.168.10.10
apiVIP: 192.168.10.10
ingressVIPs:
- 192.168.10.11
ingressVIP: 192.168.10.11
prismCentral:
  endpoint:
    address: prism.central.placeholder.address
    port: 9440
  username: username_placeholder
  password: password_placeholder
prismElements:
- endpoint:
    address: prism.element.placeholder.address
    port: 9440
  uuid: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
  name: prism_endpoint_name_placeholder
subnetUUIDs:
- yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy
`
const expectedNutanixInstallConfig412 = `apiVIPs:
- 192.168.10.10
ingressVIPs:
- 192.168.10.11
prismCentral:
  endpoint:
    address: prism.central.placeholder.address
    port: 9440
  username: username_placeholder
  password: password_placeholder
prismElements:
- endpoint:
    address: prism.element.placeholder.address
    port: 9440
  uuid: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
  name: prism_endpoint_name_placeholder
subnetUUIDs:
- yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy
`

var _ = Describe("Test GetSupportedProvidersByHosts", func() {
	bmInventory := getBaremetalInventoryStr("hostname0", "bootMode", true, false)
	vsphereInventory := getVsphereInventoryStr("hostname0", "bootMode", true, false)
	BeforeEach(func() {
		providerRegistry = InitProviderRegistry(common.GetTestLog())
		ctrl = gomock.NewController(GinkgoT())
	})
	It("no hosts", func() {
		hosts := make([]*models.Host, 0)
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(platforms).To(BeEmpty())
	})
	It("5 baremetal hosts - 3 masters, 2 workers", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, bmInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(3))
		Expect(platforms).Should(ContainElements(models.PlatformTypeBaremetal, models.PlatformTypeNone, models.PlatformTypeExternal))
	})
	It("single vsphere host", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(4))
		supportedPlatforms := []models.PlatformType{models.PlatformTypeBaremetal, models.PlatformTypeVsphere, models.PlatformTypeNone, models.PlatformTypeExternal}
		Expect(platforms).Should(ContainElements(supportedPlatforms))
	})
	It("5 vsphere hosts - 3 masters, 2 workers", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, vsphereInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(4))
		supportedPlatforms := []models.PlatformType{models.PlatformTypeBaremetal, models.PlatformTypeVsphere, models.PlatformTypeNone, models.PlatformTypeExternal}
		Expect(platforms).Should(ContainElements(supportedPlatforms))
	})
	It("2 vsphere hosts 1 generic host", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(3))
		Expect(platforms).Should(ContainElements(models.PlatformTypeBaremetal, models.PlatformTypeNone, models.PlatformTypeExternal))
	})
	It("3 vsphere masters 2 generic workers", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, bmInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(3))
		Expect(platforms).Should(ContainElements(models.PlatformTypeBaremetal, models.PlatformTypeNone, models.PlatformTypeExternal))
	})
	It("host with an invalid inventory", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, invalidInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).ToNot(BeNil())
		Expect(len(platforms)).Should(Equal(0))
	})
})

var _ = Describe("IsHostSupported", func() {
	It("platform not found", func() {
		providerRegistry = InitProviderRegistry(common.GetTestLog())
		found, err := providerRegistry.IsHostSupported("none-existing-platform-type", &models.Host{})
		Expect(err).NotTo(Succeed())
		Expect(found).To(BeFalse())
	})
})

var _ = Describe("Test AddPlatformToInstallConfig", func() {
	BeforeEach(func() {
		providerRegistry = InitProviderRegistry(common.GetTestLog())
		ctrl = gomock.NewController(GinkgoT())
	})
	Context("Unregistered Provider", func() {
		It("try to add an unregistered platform to install config", func() {
			dummyProvider := models.PlatformType("dummy")
			err := providerRegistry.AddPlatformToInstallConfig(dummyProvider, nil, nil)
			Expect(err).ToNot(BeNil())
		})
	})
	Context("baremetal", func() {
		It("test with openshift version 4.12", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname0", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname1", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname2", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getBaremetalInventoryStr("hostname3", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getBaremetalInventoryStr("hostname4", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.Cluster.OpenshiftVersion = "4.12"
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeBaremetal, &cfg, &cluster)
			Expect(err).To(BeNil())
			Expect(cfg.Platform.Baremetal).ToNot(BeNil())
			Expect(cfg.Platform.Baremetal.DeprecatedAPIVIP).To(Equal(""))
			Expect(len(cfg.Platform.Baremetal.APIVIPs)).To(Equal(len(cluster.Cluster.APIVips)))
			Expect(cfg.Platform.Baremetal.APIVIPs[0]).To(Equal(string(cluster.Cluster.APIVips[0].IP)))
			Expect(cfg.Platform.Baremetal.DeprecatedIngressVIP).To(Equal(""))
			Expect(len(cfg.Platform.Baremetal.IngressVIPs)).To(Equal(len(cluster.Cluster.IngressVips)))
			Expect(cfg.Platform.Baremetal.IngressVIPs[0]).To(Equal(string(cluster.Cluster.IngressVips[0].IP)))
			Expect(cfg.Platform.Baremetal.ProvisioningNetwork).To(Equal("Disabled"))
			Expect(len(cfg.Platform.Baremetal.Hosts)).To(Equal(len(cluster.Cluster.Hosts)))
			Expect(cfg.Platform.Baremetal.Hosts[0].Name).Should(Equal("hostname0"))
			Expect(cfg.Platform.Baremetal.Hosts[1].Name).Should(Equal("hostname1"))
			Expect(cfg.Platform.Baremetal.Hosts[2].Name).Should(Equal("hostname2"))
			Expect(cfg.Platform.Baremetal.Hosts[0].BootMACAddress).Should(Equal("EC:89:14:C0:BE:29"))
			Expect(cfg.Platform.Baremetal.Hosts[1].BootMACAddress).Should(Equal("EC:89:14:C0:BE:29"))
			Expect(cfg.Platform.Baremetal.Hosts[2].BootMACAddress).Should(Equal("EC:89:14:C0:BE:29"))
		})
		It("test with openshift version 4.8", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname0", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname1", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname2", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getBaremetalInventoryStr("hostname3", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getBaremetalInventoryStr("hostname4", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.Cluster.OpenshiftVersion = "4.8"
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeBaremetal, &cfg, &cluster)
			Expect(err).To(BeNil())
			Expect(cfg.Platform.Baremetal).ToNot(BeNil())
			Expect(cfg.Platform.Baremetal.APIVIPs[0]).To(Equal(string(cluster.Cluster.APIVips[0].IP)))
			Expect(cfg.Platform.Baremetal.IngressVIPs[0]).To(Equal(string(cluster.Cluster.IngressVips[0].IP)))
			Expect(cfg.Platform.Baremetal.ProvisioningNetwork).To(Equal("Disabled"))
			Expect(len(cfg.Platform.Baremetal.Hosts)).To(Equal(len(cluster.Cluster.Hosts)))
			Expect(cfg.Platform.Baremetal.Hosts[0].Name).Should(Equal("hostname0"))
			Expect(cfg.Platform.Baremetal.Hosts[1].Name).Should(Equal("hostname1"))
			Expect(cfg.Platform.Baremetal.Hosts[2].Name).Should(Equal("hostname2"))
			Expect(cfg.Platform.Baremetal.Hosts[0].BootMACAddress).Should(Equal("EC:89:14:C0:BE:29"))
			Expect(cfg.Platform.Baremetal.Hosts[1].BootMACAddress).Should(Equal("EC:89:14:C0:BE:29"))
			Expect(cfg.Platform.Baremetal.Hosts[2].BootMACAddress).Should(Equal("EC:89:14:C0:BE:29"))
		})
		It("test with openshift version 4.6", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname0", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname1", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname2", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getBaremetalInventoryStr("hostname3", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getBaremetalInventoryStr("hostname4", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.Cluster.OpenshiftVersion = "4.6"
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeBaremetal, &cfg, &cluster)
			Expect(err).To(BeNil())
			Expect(cfg.Platform.Baremetal).ToNot(BeNil())
			Expect(cfg.Platform.Baremetal.APIVIPs[0]).To(Equal(string(cluster.Cluster.APIVips[0].IP)))
			Expect(cfg.Platform.Baremetal.IngressVIPs[0]).To(Equal(string(cluster.Cluster.IngressVips[0].IP)))
			Expect(cfg.Platform.Baremetal.ProvisioningNetwork).To(Equal("Unmanaged"))
			Expect(len(cfg.Platform.Baremetal.Hosts)).To(Equal(len(cluster.Cluster.Hosts)))
			Expect(cfg.Platform.Baremetal.Hosts[0].Name).Should(Equal("hostname0"))
			Expect(cfg.Platform.Baremetal.Hosts[1].Name).Should(Equal("hostname1"))
			Expect(cfg.Platform.Baremetal.Hosts[2].Name).Should(Equal("hostname2"))
			Expect(cfg.Platform.Baremetal.Hosts[0].BootMACAddress).Should(Equal("EC:89:14:C0:BE:29"))
			Expect(cfg.Platform.Baremetal.Hosts[1].BootMACAddress).Should(Equal("EC:89:14:C0:BE:29"))
			Expect(cfg.Platform.Baremetal.Hosts[2].BootMACAddress).Should(Equal("EC:89:14:C0:BE:29"))
		})
		It("fails for host without interface from machine networks", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname0", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.168.1.0/24"}}
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeBaremetal, &cfg, &cluster)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).Should(ContainSubstring("Failed to find a network interface matching machine network"))
		})
		It("fails for cluster without machine networks", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname0", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.MachineNetworks = nil
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeBaremetal, &cfg, &cluster)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).Should(ContainSubstring("Failed to find machine networks for baremetal cluster"))
		})
		It("fails for cluster with empty machine networks", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname0", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.MachineNetworks = []*models.MachineNetwork{}
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeBaremetal, &cfg, &cluster)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).Should(ContainSubstring("Failed to find machine networks for baremetal cluster"))
		})
		Context("vsphere", func() {
			It("with cluster params openshift version 4.12", func() {
				cfg := getInstallerConfigBaremetal()
				hosts := make([]*models.Host, 0)
				hosts = append(hosts, createHost(true, models.HostStatusKnown, getVsphereInventoryStr("hostname0", "bootMode", true, false)))
				hosts = append(hosts, createHost(true, models.HostStatusKnown, getVsphereInventoryStr("hostname1", "bootMode", true, false)))
				hosts = append(hosts, createHost(true, models.HostStatusKnown, getVsphereInventoryStr("hostname2", "bootMode", true, false)))
				hosts = append(hosts, createHost(false, models.HostStatusKnown, getVsphereInventoryStr("hostname3", "bootMode", true, false)))
				hosts = append(hosts, createHost(false, models.HostStatusKnown, getVsphereInventoryStr("hostname4", "bootMode", true, false)))
				cluster := createClusterFromHosts(hosts)
				cluster.Cluster.OpenshiftVersion = "4.12"
				cluster.Platform = createVspherePlatformParams()
				err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeVsphere, &cfg, &cluster)
				Expect(err).To(BeNil())
				Expect(cfg.Platform.Vsphere).ToNot(BeNil())
				Expect(cfg.Platform.Vsphere.DeprecatedAPIVIP).To(Equal(""))
				Expect(len(cfg.Platform.Vsphere.APIVIPs)).To(Equal(len(cluster.Cluster.APIVips)))
				Expect(cfg.Platform.Vsphere.APIVIPs[0]).To(Equal(string(cluster.Cluster.APIVips[0].IP)))
				Expect(cfg.Platform.Vsphere.DeprecatedIngressVIP).To(Equal(""))
				Expect(len(cfg.Platform.Vsphere.IngressVIPs)).To(Equal(len(cluster.Cluster.IngressVips)))
				Expect(cfg.Platform.Vsphere.IngressVIPs[0]).To(Equal(string(cluster.Cluster.IngressVips[0].IP)))
				Expect(cfg.Platform.Vsphere.DeprecatedVCenter).To(Equal(vsphere.PhVcenter))
				Expect(cfg.Platform.Vsphere.VCenters).To(BeNil())
			})
			It("without cluster params openshift version 4.11", func() {
				cfg := getInstallerConfigBaremetal()
				hosts := make([]*models.Host, 0)
				vsphereInventory := getVsphereInventoryStr("hostname0", "bootMode", true, false)
				hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
				hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
				hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
				hosts = append(hosts, createHost(false, models.HostStatusKnown, vsphereInventory))
				hosts = append(hosts, createHost(false, models.HostStatusKnown, getVsphereInventoryStr("hostname4", "bootMode", true, false)))
				cluster := createClusterFromHosts(hosts)
				cluster.Cluster.OpenshiftVersion = "4.11"
				cluster.Platform = createVspherePlatformParams()
				err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeVsphere, &cfg, &cluster)
				Expect(err).To(BeNil())
				Expect(cfg.Platform.Vsphere).ToNot(BeNil())
				Expect(cfg.Platform.Vsphere.APIVIPs[0]).To(Equal(string(cluster.Cluster.APIVips[0].IP)))
				Expect(cfg.Platform.Vsphere.IngressVIPs[0]).To(Equal(string(cluster.Cluster.IngressVips[0].IP)))
				Expect(cfg.Platform.Vsphere.DeprecatedVCenter).To(Equal(vsphere.PhVcenter))
				Expect(cfg.Platform.Vsphere.VCenters).To(BeNil())
			})
			It("vsphere default values before openshift version 4.13", func() {
				cfg := getInstallerConfigBaremetal()
				cluster := createClusterFromHosts([]*models.Host{})
				cluster.Cluster.OpenshiftVersion = "4.11"
				cluster.Platform = createVspherePlatformParams()
				err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeVsphere, &cfg, &cluster)
				Expect(err).To(BeNil())
				Expect(cfg.Platform.Vsphere).ToNot(BeNil())
				Expect(cfg.Platform.Vsphere.APIVIPs[0]).To(Equal(string(cluster.Cluster.APIVips[0].IP)))
				Expect(cfg.Platform.Vsphere.IngressVIPs[0]).To(Equal(string(cluster.Cluster.IngressVips[0].IP)))
				Expect(cfg.Platform.Vsphere.DeprecatedVCenter).To(Equal(vsphere.PhVcenter))
				Expect(cfg.Platform.Vsphere.DeprecatedCluster).To(Equal(vsphere.PhCluster))
				Expect(cfg.Platform.Vsphere.DeprecatedNetwork).To(Equal(vsphere.PhNetwork))
				Expect(cfg.Platform.Vsphere.DeprecatedUsername).To(Equal(vsphere.PhUsername))
				Expect(string(cfg.Platform.Vsphere.DeprecatedPassword)).To(Equal(vsphere.PhPassword))
				Expect(cfg.Platform.Vsphere.DeprecatedDefaultDatastore).To(Equal(vsphere.PhDefaultDatastore))
				Expect(cfg.Platform.Vsphere.DeprecatedDatacenter).To(Equal(vsphere.PhDatacenter))
				Expect(cfg.Platform.Vsphere.VCenters).To(BeNil())
				Expect(cfg.Platform.Vsphere.FailureDomains).To(BeNil())
			})
			It("vsphere default values after openshift version 4.13", func() {
				cfg := getInstallerConfigBaremetal()
				cluster := createClusterFromHosts([]*models.Host{})
				cluster.Cluster.OpenshiftVersion = "4.13"
				cluster.Platform = createVspherePlatformParams()
				err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeVsphere, &cfg, &cluster)
				Expect(err).To(BeNil())
				Expect(cfg.Platform.Vsphere).ToNot(BeNil())
				Expect(cfg.Platform.Vsphere.APIVIPs).To(Not(BeNil()))
				Expect(cfg.Platform.Vsphere.IngressVIPs).To(Not(BeNil()))
				Expect(cfg.Platform.Vsphere.DeprecatedIngressVIP).To(Equal(""))
				Expect(cfg.Platform.Vsphere.DeprecatedVCenter).To(Equal(""))
				Expect(cfg.Platform.Vsphere.DeprecatedCluster).To(Equal(""))
				Expect(cfg.Platform.Vsphere.DeprecatedNetwork).To(Equal(""))
				Expect(cfg.Platform.Vsphere.DeprecatedUsername).To(Equal(""))
				Expect(string(cfg.Platform.Vsphere.DeprecatedPassword)).To(Equal(""))
				Expect(cfg.Platform.Vsphere.DeprecatedDefaultDatastore).To(Equal(""))
				Expect(cfg.Platform.Vsphere.DeprecatedDatacenter).To(Equal(""))

				Expect(cfg.Platform.Vsphere.VCenters[0].Server).To(Equal(vsphere.PhVcenter))
				Expect(cfg.Platform.Vsphere.VCenters[0].Datacenters[0]).To(Equal(vsphere.PhDatacenter))
				Expect(cfg.Platform.Vsphere.VCenters[0].Username).To(Equal(vsphere.PhUsername))
				Expect(string(cfg.Platform.Vsphere.VCenters[0].Password)).To(Equal(vsphere.PhPassword))

				Expect(cfg.Platform.Vsphere.FailureDomains[0].Topology.ComputeCluster).To(Equal(fmt.Sprintf("/%s/host/%s", vsphere.PhDatacenter, vsphere.PhCluster)))
				Expect(cfg.Platform.Vsphere.FailureDomains[0].Topology.Datacenter).To(Equal(vsphere.PhDatacenter))
				Expect(cfg.Platform.Vsphere.FailureDomains[0].Topology.Datastore).To(Equal(fmt.Sprintf("/%s/datastore/%s", vsphere.PhDatacenter, vsphere.PhDefaultDatastore)))
				Expect(cfg.Platform.Vsphere.FailureDomains[0].Topology.Folder).To(Equal(fmt.Sprintf("/%s/vm/%s", vsphere.PhDatacenter, vsphere.PhFolder)))
				Expect(cfg.Platform.Vsphere.FailureDomains[0].Topology.Networks[0]).To(Equal(vsphere.PhNetwork))
				Expect(cfg.Platform.Vsphere.FailureDomains[0].Server).To(Equal(vsphere.PhVcenter))
			})
		})
	})

	Context("nutanix", func() {
		It("without cluster params openshift version 4.12", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getNutanixInventoryStr("hostname0", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getNutanixInventoryStr("hostname1", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getNutanixInventoryStr("hostname2", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getNutanixInventoryStr("hostname3", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getNutanixInventoryStr("hostname4", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.Cluster.OpenshiftVersion = "4.12"
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeNutanix, &cfg, &cluster)
			Expect(err).To(BeNil())
			Expect(cfg.Platform.Nutanix).ToNot(BeNil())
			installConfigByte, err := yaml.Marshal(cfg.Platform.Nutanix)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(installConfigByte)).To(Equal(expectedNutanixInstallConfig412))
		})
		It("without cluster params openshift version 4.11", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getNutanixInventoryStr("hostname0", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getNutanixInventoryStr("hostname1", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getNutanixInventoryStr("hostname2", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getNutanixInventoryStr("hostname3", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getNutanixInventoryStr("hostname4", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.Cluster.OpenshiftVersion = "4.11"
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeNutanix, &cfg, &cluster)
			Expect(err).To(BeNil())
			Expect(cfg.Platform.Nutanix).ToNot(BeNil())
			installConfigByte, err := yaml.Marshal(cfg.Platform.Nutanix)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(installConfigByte)).To(Equal(expectedNutanixInstallConfig411))
		})
	})

	Context("oci", func() {
		It("should set platform name to oci", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname0", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname1", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname2", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getBaremetalInventoryStr("hostname3", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getBaremetalInventoryStr("hostname4", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeOci, &cfg, &cluster)
			Expect(err).To(BeNil())
			Expect(cfg.Platform.External).ToNot(BeNil())
			Expect(cfg.Platform.External.PlatformName).To(Equal(string(models.PlatformTypeOci)))
		})
	})
})

var _ = Describe("Test SetPlatformUsages", func() {
	var (
		usageApi *usage.MockAPI
	)
	BeforeEach(func() {
		providerRegistry = InitProviderRegistry(common.GetTestLog())
		ctrl = gomock.NewController(GinkgoT())
		usageApi = usage.NewMockAPI(ctrl)
	})
	Context("Unregistered Provider", func() {
		It("try to with an unregistered provider", func() {
			dummyProvider := models.PlatformType("dummy")
			err := providerRegistry.SetPlatformUsages(dummyProvider, nil, usageApi)
			Expect(err).ToNot(BeNil())
		})
	})
	Context("baremetal", func() {
		It("success", func() {
			usageApi.EXPECT().Remove(gomock.Any(), gomock.Any()).AnyTimes()
			err := providerRegistry.SetPlatformUsages(models.PlatformTypeBaremetal, nil, usageApi)
			Expect(err).To(BeNil())
		})
	})
	Context("vsphere", func() {
		It("success", func() {
			usageApi.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			err := providerRegistry.SetPlatformUsages(models.PlatformTypeVsphere, nil, usageApi)
			Expect(err).To(BeNil())
		})
	})
	Context("oci", func() {
		It("success", func() {
			usageApi.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			err := providerRegistry.SetPlatformUsages(models.PlatformTypeOci, nil, usageApi)
			Expect(err).To(BeNil())
		})
	})
})

func createHost(isMaster bool, state string, inventory string) *models.Host {
	hostId := strfmt.UUID(uuid.New().String())
	clusterId := strfmt.UUID(uuid.New().String())
	infraEnvId := strfmt.UUID(uuid.New().String())
	hostRole := models.HostRoleWorker
	if isMaster {
		hostRole = models.HostRoleMaster
	}
	host := models.Host{
		ID:         &hostId,
		InfraEnvID: infraEnvId,
		ClusterID:  &clusterId,
		Kind:       swag.String(models.HostKindHost),
		Status:     swag.String(state),
		Role:       hostRole,
		Inventory:  inventory,
	}
	return &host
}

func getInventory(hostname, bootMode string, ipv4, ipv6 bool) models.Inventory {
	inventory := models.Inventory{
		Hostname: hostname,
		Boot:     &models.Boot{CurrentBootMode: bootMode},
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: []string{},
				IPV6Addresses: []string{},
				MacAddress:    "EC:89:14:C0:BE:29",
				Type:          "physical",
			},
			{
				IPV4Addresses: []string{},
				IPV6Addresses: []string{},
				MacAddress:    "DC:86:D8:97:C4:1A",
				Type:          "physical",
			},
		},
	}
	if ipv4 {
		inventory.Interfaces[0].IPV4Addresses = []string{"10.35.20.10/24"}
	}
	if ipv6 {
		inventory.Interfaces[0].IPV6Addresses = []string{"fe80::1/64"}
	}
	return inventory
}

func getVsphereInventoryStr(hostname, bootMode string, ipv4, ipv6 bool) string {
	inventory := getInventory(hostname, bootMode, ipv4, ipv6)
	inventory.SystemVendor = &models.SystemVendor{
		Manufacturer: "VMware, Inc.",
		ProductName:  "Mware7,1",
		SerialNumber: "VMware-12 34 56 78 90 12 ab cd-ef gh 12 34 56 67 89 90",
		Virtual:      true,
	}
	ret, _ := json.Marshal(&inventory)
	return string(ret)
}

func getBaremetalInventoryStr(hostname, bootMode string, ipv4, ipv6 bool) string {
	inventory := getInventory(hostname, bootMode, ipv4, ipv6)
	inventory.SystemVendor = &models.SystemVendor{
		Manufacturer: "Red Hat",
		ProductName:  "KVM",
		SerialNumber: "",
		Virtual:      false,
	}
	ret, _ := json.Marshal(&inventory)
	return string(ret)
}

func getNutanixInventoryStr(hostname, bootMode string, ipv4, ipv6 bool) string {
	inventory := getInventory(hostname, bootMode, ipv4, ipv6)
	inventory.SystemVendor = &models.SystemVendor{
		Manufacturer: "Nutanix",
		ProductName:  "AHV",
		SerialNumber: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
		Virtual:      true,
	}
	ret, _ := json.Marshal(&inventory)
	return string(ret)
}

func createVspherePlatformParams() *models.Platform {
	return &models.Platform{
		Type: common.PlatformTypePtr(models.PlatformTypeVsphere),
	}
}

func createClusterFromHosts(hosts []*models.Host) common.Cluster {
	clusterID := strfmt.UUID(uuid.New().String())
	return common.Cluster{
		Cluster: models.Cluster{
			Name:             "cluster",
			ID:               &clusterID,
			APIVips:          []*models.APIVip{{IP: "192.168.10.10"}},
			Hosts:            hosts,
			IngressVips:      []*models.IngressVip{{IP: "192.168.10.11"}},
			OpenshiftVersion: "4.7",
			CPUArchitecture:  models.ClusterCPUArchitectureX8664,
			MachineNetworks:  []*models.MachineNetwork{{Cidr: "10.35.20.0/24"}},
		},
	}
}

func getInstallerConfigBaremetal() installcfg.InstallerConfigBaremetal {
	return installcfg.InstallerConfigBaremetal{
		APIVersion: "v1",
		BaseDomain: "test.base.domain",
		Networking: struct {
			NetworkType    string                      `json:"networkType"`
			ClusterNetwork []installcfg.ClusterNetwork `json:"clusterNetwork"`
			MachineNetwork []installcfg.MachineNetwork `json:"machineNetwork,omitempty"`
			ServiceNetwork []string                    `json:"serviceNetwork"`
		}{
			NetworkType:    "OpenShiftSDN",
			ClusterNetwork: []installcfg.ClusterNetwork{{Cidr: "10.128.0.0/14", HostPrefix: 23}},
			MachineNetwork: []installcfg.MachineNetwork{{Cidr: "10.0.0.0/16"}},
			ServiceNetwork: []string{"172.30.0.0/16"},
		},
		Metadata: struct {
			Name string `json:"name"`
		}{Name: "dummy"},
		Compute: []struct {
			Hyperthreading string `json:"hyperthreading,omitempty"`
			Name           string `json:"name"`
			Replicas       int    `json:"replicas"`
		}{{
			Name:     "worker-test",
			Replicas: 2,
		}},
		ControlPlane: struct {
			Hyperthreading string `json:"hyperthreading,omitempty"`
			Name           string `json:"name"`
			Replicas       int    `json:"replicas"`
		}{
			Name:     "master-test",
			Replicas: 3,
		},
		Platform:              installcfg.Platform{},
		BootstrapInPlace:      &installcfg.BootstrapInPlace{},
		FIPS:                  false,
		PullSecret:            "{\"auths\": fake}",
		SSHKey:                "ssh-rsa fake",
		AdditionalTrustBundle: "",
		ImageContentSources:   nil,
	}
}

func TestProviderRegistry(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ProviderRegistry test")
}
