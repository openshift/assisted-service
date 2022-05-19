package registry

import (
	context "context"
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
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/internal/provider/ovirt"
	"github.com/openshift/assisted-service/internal/provider/vsphere"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
)

var (
	providerRegistry ProviderRegistry
	ctrl             *gomock.Controller
)

const invalidInventory = "{\"system_vendor\": \"invalid\"}"

const ovirtFqdn = "ovirt.example.com"
const ovirtUsername = "admin@internal"
const ovirtPassword = "redhat"
const ovirtClusterID = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
const ovirtStorageDomainID = "yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy"
const ovirtNetworkName = "ovirtmgmt"
const ovirtVnicProfileID = "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz"
const ovirtInsecure = false
const ovirtCaBundle = `
subject=C = US, ST = North Carolina, L = Raleigh, O = "Red Hat, Inc.",\
OU = Red Hat IT, CN = Red Hat IT Root CA, emailAddress = infosec@redhat.com

issuer=C = US, ST = North Carolina, L = Raleigh, O = "Red Hat, Inc.",\
OU = Red Hat IT, CN = Red Hat IT Root CA, emailAddress = infosec@redhat.com

-----BEGIN CERTIFICATE-----
MIIENDCCAxygAwIBAgIJANunI0D662cnMA0GCSqGSIb3DQEBCwUAMIGlMQswCQYD
VQQGEwJVUzEXMBUGA1UECAwOTm9ydGggQ2Fyb2xpbmExEDAOBgNVBAcMB1JhbGVp
Z2gxFjAUBgNVBAoMDVJlZCBIYXQsIEluYy4xEzARBgNVBAsMClJlZCBIYXQgSVQx
GzAZBgNVBAMMElJlZCBIYXQgSVQgUm9vdCBDQTEhMB8GCSqGSIb3DQEJARYSaW5m
b3NlY0ByZWRoYXQuY29tMCAXDTE1MDcwNjE3MzgxMVoYDzIwNTUwNjI2MTczODEx
WjCBpTELMAkGA1UEBhMCVVMxFzAVBgNVBAgMDk5vcnRoIENhcm9saW5hMRAwDgYD
VQQHDAdSYWxlaWdoMRYwFAYDVQQKDA1SZWQgSGF0LCBJbmMuMRMwEQYDVQQLDApS
ZWQgSGF0IElUMRswGQYDVQQDDBJSZWQgSGF0IElUIFJvb3QgQ0ExITAfBgkqhkiG
9w0BCQEWEmluZm9zZWNAcmVkaGF0LmNvbTCCASIwDQYJKoZIhvcNAQEBBQADggEP
ADCCAQoCggEBALQt9OJQh6GC5LT1g80qNh0u50BQ4sZ/yZ8aETxt+5lnPVX6MHKz
bfwI6nO1aMG6j9bSw+6UUyPBHP796+FT/pTS+K0wsDV7c9XvHoxJBJJU38cdLkI2
c/i7lDqTfTcfLL2nyUBd2fQDk1B0fxrskhGIIZ3ifP1Ps4ltTkv8hRSob3VtNqSo
GxkKfvD2PKjTPxDPWYyruy9irLZioMffi3i/gCut0ZWtAyO3MVH5qWF/enKwgPES
X9po+TdCvRB/RUObBaM761EcrLSM1GqHNueSfqnho3AjLQ6dBnPWlo638Zm1VebK
BELyhkLWMSFkKwDmne0jQ02Y4g075vCKvCsCAwEAAaNjMGEwHQYDVR0OBBYEFH7R
4yC+UehIIPeuL8Zqw3PzbgcZMB8GA1UdIwQYMBaAFH7R4yC+UehIIPeuL8Zqw3Pz
bgcZMA8GA1UdEwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgGGMA0GCSqGSIb3DQEB
CwUAA4IBAQBDNvD2Vm9sA5A9AlOJR8+en5Xz9hXcxJB5phxcZQ8jFoG04Vshvd0e
LEnUrMcfFgIZ4njMKTQCM4ZFUPAieyLx4f52HuDopp3e5JyIMfW+KFcNIpKwCsak
oSoKtIUOsUJK7qBVZxcrIyeQV2qcYOeZhtS5wBqIwOAhFwlCET7Ze58QHmS48slj
S9K0JAcps2xdnGu0fkzhSQxY8GPQNFTlr6rYld5+ID/hHeS76gq0YG3q6RLWRkHf
4eTkRjivAlExrFzKcljC4axKQlnOvVAzz+Gm32U0xPBF4ByePVxCJUHw1TsyTmel
RxNEp7yHoXcwn+fXna+t5JWh1gxUZty3
-----END CERTIFICATE-----
`

var _ = Describe("Test GetSupportedProvidersByHosts", func() {
	var (
		manifestsApi *manifestsapi.MockManifestsAPI
	)
	bmInventory := getBaremetalInventoryStr("hostname0", "bootMode", true, false)
	vsphereInventory := getVsphereInventoryStr("hostname0", "bootMode", true, false)
	ovirtInventory := getOvirtInventoryStr("hostname0", "bootMode", true, false)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		manifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
		providerRegistry = NewProviderRegistry(manifestsApi)
		providerRegistry.InitProviders(common.GetTestLog())
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
		Expect(len(platforms)).Should(Equal(1))
		Expect(platforms[0]).Should(Equal(models.PlatformTypeBaremetal))
	})
	It("single vsphere host", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(2))
		supportedPlatforms := []models.PlatformType{models.PlatformTypeBaremetal, models.PlatformTypeVsphere}
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
		Expect(len(platforms)).Should(Equal(2))
		supportedPlatforms := []models.PlatformType{models.PlatformTypeBaremetal, models.PlatformTypeVsphere}
		Expect(platforms).Should(ContainElements(supportedPlatforms))
	})
	It("2 vsphere hosts 1 generic host", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(1))
		Expect(platforms[0]).Should(Equal(models.PlatformTypeBaremetal))
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
		Expect(len(platforms)).Should(Equal(1))
		Expect(platforms[0]).Should(Equal(models.PlatformTypeBaremetal))
	})
	It("single ovirt host", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(2))
		supportedPlatforms := []models.PlatformType{models.PlatformTypeBaremetal, models.PlatformTypeOvirt}
		Expect(platforms).Should(ContainElements(supportedPlatforms))
	})
	It("5 ovirt hosts - 3 masters, 2 workers", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, ovirtInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(2))
		supportedPlatforms := []models.PlatformType{models.PlatformTypeBaremetal, models.PlatformTypeOvirt}
		Expect(platforms).Should(ContainElements(supportedPlatforms))
	})
	It("2 ovirt hosts 1 generic host", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(1))
		Expect(platforms[0]).Should(Equal(models.PlatformTypeBaremetal))
	})
	It("3 ovirt masters 2 generic workers", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, bmInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(1))
		Expect(platforms[0]).Should(Equal(models.PlatformTypeBaremetal))
	})
	It("host with an invalid inventory", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, invalidInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).ToNot(BeNil())
		Expect(len(platforms)).Should(Equal(0))
	})
})

var _ = Describe("Test AddPlatformToInstallConfig", func() {
	var (
		manifestsApi *manifestsapi.MockManifestsAPI
	)
	BeforeEach(func() {
		providerRegistry = NewProviderRegistry(manifestsApi)
		providerRegistry.InitProviders(common.GetTestLog())
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
		It("test with openshift greater then 4.7", func() {
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
			Expect(cfg.Platform.Baremetal.APIVIP).To(Equal(cluster.Cluster.APIVip))
			Expect(cfg.Platform.Baremetal.IngressVIP).To(Equal(cluster.Cluster.IngressVip))
			Expect(cfg.Platform.Baremetal.ProvisioningNetwork).To(Equal("Disabled"))
			Expect(len(cfg.Platform.Baremetal.Hosts)).To(Equal(len(cluster.Cluster.Hosts)))
			Expect(cfg.Platform.Baremetal.Hosts[0].Name).Should(Equal("hostname0"))
			Expect(cfg.Platform.Baremetal.Hosts[1].Name).Should(Equal("hostname1"))
			Expect(cfg.Platform.Baremetal.Hosts[2].Name).Should(Equal("hostname2"))
		})
		It("test with openshift version less 4.7", func() {
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
			Expect(cfg.Platform.Baremetal.APIVIP).To(Equal(cluster.Cluster.APIVip))
			Expect(cfg.Platform.Baremetal.IngressVIP).To(Equal(cluster.Cluster.IngressVip))
			Expect(cfg.Platform.Baremetal.ProvisioningNetwork).To(Equal("Unmanaged"))
			Expect(len(cfg.Platform.Baremetal.Hosts)).To(Equal(len(cluster.Cluster.Hosts)))
			Expect(cfg.Platform.Baremetal.Hosts[0].Name).Should(Equal("hostname0"))
			Expect(cfg.Platform.Baremetal.Hosts[1].Name).Should(Equal("hostname1"))
			Expect(cfg.Platform.Baremetal.Hosts[2].Name).Should(Equal("hostname2"))
		})
		Context("vsphere", func() {
			It("with cluster params", func() {
				cfg := getInstallerConfigBaremetal()
				hosts := make([]*models.Host, 0)
				hosts = append(hosts, createHost(true, models.HostStatusKnown, getVsphereInventoryStr("hostname0", "bootMode", true, false)))
				hosts = append(hosts, createHost(true, models.HostStatusKnown, getVsphereInventoryStr("hostname1", "bootMode", true, false)))
				hosts = append(hosts, createHost(true, models.HostStatusKnown, getVsphereInventoryStr("hostname2", "bootMode", true, false)))
				hosts = append(hosts, createHost(false, models.HostStatusKnown, getVsphereInventoryStr("hostname3", "bootMode", true, false)))
				hosts = append(hosts, createHost(false, models.HostStatusKnown, getVsphereInventoryStr("hostname4", "bootMode", true, false)))
				cluster := createClusterFromHosts(hosts)
				cluster.Platform = createVspherePlatformParams()
				err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeVsphere, &cfg, &cluster)
				Expect(err).To(BeNil())
				Expect(cfg.Platform.Vsphere).ToNot(BeNil())
				Expect(cfg.Platform.Vsphere.APIVIP).To(Equal(cluster.Cluster.APIVip))
				Expect(cfg.Platform.Vsphere.IngressVIP).To(Equal(cluster.Cluster.IngressVip))
				Expect(cfg.Platform.Vsphere.VCenter).To(Equal(vsphere.PhVcenter))
			})
			It("without cluster params", func() {
				cfg := getInstallerConfigBaremetal()
				hosts := make([]*models.Host, 0)
				vsphereInventory := getVsphereInventoryStr("hostname0", "bootMode", true, false)
				hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
				hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
				hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
				hosts = append(hosts, createHost(false, models.HostStatusKnown, vsphereInventory))
				hosts = append(hosts, createHost(false, models.HostStatusKnown, getVsphereInventoryStr("hostname4", "bootMode", true, false)))
				cluster := createClusterFromHosts(hosts)
				cluster.Platform = createVspherePlatformParams()
				err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeVsphere, &cfg, &cluster)
				Expect(err).To(BeNil())
				Expect(cfg.Platform.Vsphere).ToNot(BeNil())
				Expect(cfg.Platform.Vsphere.APIVIP).To(Equal(cluster.Cluster.APIVip))
				Expect(cfg.Platform.Vsphere.IngressVIP).To(Equal(cluster.Cluster.IngressVip))
				Expect(cfg.Platform.Vsphere.VCenter).To(Equal(vsphere.PhVcenter))
			})
		})
	})
	Context("ovirt", func() {
		It("with cluster params", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getOvirtInventoryStr("hostname0", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getOvirtInventoryStr("hostname1", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getOvirtInventoryStr("hostname2", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getOvirtInventoryStr("hostname3", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getOvirtInventoryStr("hostname4", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.Platform = createOvirtPlatformParams()
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeOvirt, &cfg, &cluster)
			Expect(err).To(BeNil())
			Expect(cfg.Platform.Ovirt).ToNot(BeNil())
			Expect(cfg.Platform.Ovirt.APIVIP).To(Equal(cluster.Cluster.APIVip))
			Expect(cfg.Platform.Ovirt.IngressVIP).To(Equal(cluster.Cluster.IngressVip))
			Expect(cfg.Platform.Ovirt.ClusterID.String()).To(Equal(ovirtClusterID))
			Expect(cfg.Platform.Ovirt.StorageDomainID.String()).To(Equal(ovirtStorageDomainID))
			Expect(cfg.Platform.Ovirt.NetworkName).To(Equal(ovirtNetworkName))
			Expect(cfg.Platform.Ovirt.VnicProfileID.String()).To(Equal(ovirtVnicProfileID))
		})
		It("without cluster params", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			ovirtInventory := getOvirtInventoryStr("hostname0", "bootMode", true, false)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, ovirtInventory))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getOvirtInventoryStr("hostname4", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.Platform = createOvirtPlatformParams()
			cluster.Platform.Ovirt = nil
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeOvirt, &cfg, &cluster)
			Expect(err).To(BeNil())
			Expect(cfg.Platform.Ovirt).To(BeNil())
		})
	})
})

var _ = Describe("Test SetPlatformValuesInDBUpdates", func() {
	var (
		manifestsApi *manifestsapi.MockManifestsAPI
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		manifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
		providerRegistry = NewProviderRegistry(manifestsApi)
		providerRegistry.InitProviders(common.GetTestLog())
	})
	Context("Unregistered Provider", func() {
		It("try to with an unregistered provider", func() {
			dummyProvider := models.PlatformType("dummy")
			err := providerRegistry.SetPlatformValuesInDBUpdates(dummyProvider, nil, nil)
			Expect(err).ToNot(BeNil())
		})
	})
	Context("ovirt", func() {
		It("set from empty updates", func() {
			platformParams := createOvirtPlatformParams()
			updates := make(map[string]interface{})
			err := providerRegistry.SetPlatformValuesInDBUpdates(models.PlatformTypeOvirt, platformParams, updates)
			Expect(err).To(BeNil())
			Expect(updates).ShouldNot(BeNil())
			Expect(updates[ovirt.DbFieldUsername]).To(Equal(platformParams.Ovirt.Username))
		})
		It("switch from ovirt to bare metal", func() {
			platformParams := createOvirtPlatformParams()
			updates := make(map[string]interface{})
			err := providerRegistry.SetPlatformValuesInDBUpdates(models.PlatformTypeOvirt, platformParams, updates)
			Expect(err).To(BeNil())
			Expect(updates).ShouldNot(BeNil())
			Expect(updates[ovirt.DbFieldUsername]).To(Equal(platformParams.Ovirt.Username))
			err = providerRegistry.SetPlatformValuesInDBUpdates(models.PlatformTypeBaremetal, platformParams, updates)
			Expect(err).To(BeNil())
			Expect(updates).ShouldNot(BeNil())
			Expect(updates[ovirt.DbFieldUsername]).To(BeNil())
		})
	})
})

var _ = Describe("Test SetPlatformUsages", func() {
	var (
		usageApi     *usage.MockAPI
		manifestsApi *manifestsapi.MockManifestsAPI
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		usageApi = usage.NewMockAPI(ctrl)
		manifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
		providerRegistry = NewProviderRegistry(manifestsApi)
		providerRegistry.InitProviders(common.GetTestLog())
	})
	Context("Unregistered Provider", func() {
		It("try to with an unregistered provider", func() {
			dummyProvider := models.PlatformType("dummy")
			err := providerRegistry.SetPlatformUsages(dummyProvider, nil, nil, usageApi)
			Expect(err).ToNot(BeNil())
		})
	})
	Context("baremetal", func() {
		It("success", func() {
			usageApi.EXPECT().Remove(gomock.Any(), gomock.Any()).AnyTimes()
			err := providerRegistry.SetPlatformUsages(models.PlatformTypeBaremetal, nil, nil, usageApi)
			Expect(err).To(BeNil())
		})
	})
	Context("vsphere", func() {
		It("success", func() {
			usageApi.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			platformParams := createVspherePlatformParams()
			err := providerRegistry.SetPlatformUsages(models.PlatformTypeVsphere, platformParams, nil, usageApi)
			Expect(err).To(BeNil())
		})
	})
	Context("ovirt", func() {
		It("success", func() {
			usageApi.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			platformParams := createOvirtPlatformParams()
			err := providerRegistry.SetPlatformUsages(models.PlatformTypeOvirt, platformParams, nil, usageApi)
			Expect(err).To(BeNil())
		})
	})
})

var _ = Describe("Test GetActualSchedulableMasters", func() {
	var (
		manifestsApi *manifestsapi.MockManifestsAPI
	)
	hostStatusKnown := models.HostStatusKnown
	platforms := []models.PlatformType{
		models.PlatformTypeBaremetal,
		models.PlatformTypeVsphere,
		models.PlatformTypeOvirt,
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		manifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
		providerRegistry = NewProviderRegistry(manifestsApi)
		providerRegistry.InitProviders(common.GetTestLog())
	})

	Context("Wrong parameters", func() {
		It("nil cluster", func() {
			_, err := providerRegistry.GetActualSchedulableMasters(nil)
			Expect(err).NotTo(BeNil())
		})
		It("Empty cluster", func() {
			cluster := &common.Cluster{}
			_, err := providerRegistry.GetActualSchedulableMasters(cluster)
			Expect(err).NotTo(BeNil())
		})
		It("Unknown platform", func() {
			cluster := &common.Cluster{}
			cluster.Platform = &models.Platform{Type: models.NewPlatformType("NotRegisteredPlatform")}
			_, err := providerRegistry.GetActualSchedulableMasters(cluster)
			Expect(err).NotTo(BeNil())
		})
	})

	for _, current_platform := range platforms {
		platform := current_platform

		Context(fmt.Sprintf("%v platform", platform), func() {
			It("no hosts", func() {
				hosts := make([]*models.Host, 0)
				cluster := createClusterFromHostsPlatform(hosts, platform)
				By("Default value")
				schedulableMasters, err := providerRegistry.GetActualSchedulableMasters(&cluster)
				Expect(err).To(BeNil())
				if platform == models.PlatformTypeBaremetal {
					Expect(schedulableMasters).To(BeTrue())
				} else {
					Expect(schedulableMasters).To(BeFalse())
				}
				By("Enable schedulable masters")
				cluster.SchedulableMasters = swag.Bool(true)
				schedulableMasters, err = providerRegistry.GetActualSchedulableMasters(&cluster)
				Expect(err).To(BeNil())
				Expect(schedulableMasters).To(BeTrue())
				By("Disable schedulable masters")
				cluster.SchedulableMasters = swag.Bool(false)
				schedulableMasters, err = providerRegistry.GetActualSchedulableMasters(&cluster)
				Expect(err).To(BeNil())
				Expect(schedulableMasters).To(BeFalse())
			})
			It("3 masters", func() {
				hosts := createHosts(3, 0, hostStatusKnown, platform)
				cluster := createClusterFromHostsPlatform(hosts, platform)
				By("Default value")
				schedulableMasters, err := providerRegistry.GetActualSchedulableMasters(&cluster)
				Expect(err).To(BeNil())
				if platform == models.PlatformTypeBaremetal {
					Expect(schedulableMasters).To(BeTrue())
				} else {
					Expect(schedulableMasters).To(BeFalse())
				}
				By("Enable schedulable masters")
				cluster.SchedulableMasters = swag.Bool(true)
				schedulableMasters, err = providerRegistry.GetActualSchedulableMasters(&cluster)
				Expect(err).To(BeNil())
				Expect(schedulableMasters).To(BeTrue())
				By("Disable schedulable masters")
				cluster.SchedulableMasters = swag.Bool(false)
				schedulableMasters, err = providerRegistry.GetActualSchedulableMasters(&cluster)
				Expect(err).To(BeNil())
				Expect(schedulableMasters).To(BeFalse())
			})
			It("3 masters - 1 worker", func() {
				hosts := createHosts(3, 1, hostStatusKnown, platform)
				cluster := createClusterFromHostsPlatform(hosts, platform)
				By("Default value")
				schedulableMasters, err := providerRegistry.GetActualSchedulableMasters(&cluster)
				Expect(err).To(BeNil())
				if platform == models.PlatformTypeBaremetal {
					Expect(schedulableMasters).To(BeTrue())
				} else {
					Expect(schedulableMasters).To(BeFalse())
				}
				By("Enable schedulable masters")
				cluster.SchedulableMasters = swag.Bool(true)
				schedulableMasters, err = providerRegistry.GetActualSchedulableMasters(&cluster)
				Expect(err).To(BeNil())
				Expect(schedulableMasters).To(BeTrue())
				By("Disable schedulable masters")
				cluster.SchedulableMasters = swag.Bool(false)
				schedulableMasters, err = providerRegistry.GetActualSchedulableMasters(&cluster)
				Expect(err).To(BeNil())
				Expect(schedulableMasters).To(BeFalse())
			})
			It("3 masters - 2 workers", func() {
				hosts := createHosts(3, 2, hostStatusKnown, platform)
				cluster := createClusterFromHostsPlatform(hosts, platform)
				By("Default value")
				schedulableMasters, err := providerRegistry.GetActualSchedulableMasters(&cluster)
				Expect(err).To(BeNil())
				Expect(schedulableMasters).To(BeFalse())
				By("Enable schedulable masters")
				cluster.SchedulableMasters = swag.Bool(true)
				schedulableMasters, err = providerRegistry.GetActualSchedulableMasters(&cluster)
				Expect(err).To(BeNil())
				Expect(schedulableMasters).To(BeTrue())
				By("Disable schedulable masters")
				cluster.SchedulableMasters = swag.Bool(false)
				schedulableMasters, err = providerRegistry.GetActualSchedulableMasters(&cluster)
				Expect(err).To(BeNil())
				Expect(schedulableMasters).To(BeFalse())
			})
		})
	}
})

var _ = Describe("Test GenerateProviderManifests", func() {
	var (
		ctx          = context.Background()
		manifestsApi *manifestsapi.MockManifestsAPI
	)
	hostStatusKnown := models.HostStatusKnown
	platforms := []models.PlatformType{
		models.PlatformTypeBaremetal,
		models.PlatformTypeVsphere,
		models.PlatformTypeOvirt,
	}
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		manifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
		providerRegistry = NewProviderRegistry(manifestsApi)
		providerRegistry.InitProviders(common.GetTestLog())
		manifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any()).Times(3)
	})
	for _, current_platform := range platforms {
		platform := current_platform
		Context(fmt.Sprintf("%v platform", platform), func() {
			It("no hosts", func() {
				By("Default value")
				cluster := createClusterFromHosts(nil)
				err := providerRegistry.GenerateProviderManifests(ctx, &cluster)
				Expect(err).NotTo(BeNil())
				By("Enable schedulable masters")
				cluster.SchedulableMasters = swag.Bool(true)
				err = providerRegistry.GenerateProviderManifests(ctx, &cluster)
				Expect(err).NotTo(BeNil())
				By("Disable schedulable masters")
				cluster.SchedulableMasters = swag.Bool(false)
				err = providerRegistry.GenerateProviderManifests(ctx, &cluster)
				Expect(err).NotTo(BeNil())
			})
			It("3 masters", func() {
				hosts := createHosts(3, 0, hostStatusKnown, platform)
				cluster := createClusterFromHostsPlatform(hosts, platform)
				cluster.SchedulableMasters = swag.Bool(true)
				err := providerRegistry.GenerateProviderManifests(ctx, &cluster)
				Expect(err).To(BeNil())
				// Enable schedulable masters
				cluster.SchedulableMasters = swag.Bool(true)
				err = providerRegistry.GenerateProviderManifests(ctx, &cluster)
				Expect(err).To(BeNil())
				// Disable schedulable masters
				cluster.SchedulableMasters = swag.Bool(false)
				err = providerRegistry.GenerateProviderManifests(ctx, &cluster)
				Expect(err).To(BeNil())
			})
			It("3 masters - 2 workers", func() {
				hosts := createHosts(3, 2, hostStatusKnown, platform)
				cluster := createClusterFromHostsPlatform(hosts, platform)
				err := providerRegistry.GenerateProviderManifests(ctx, &cluster)
				Expect(err).To(BeNil())
				// Enable schedulable masters
				cluster.SchedulableMasters = swag.Bool(true)
				err = providerRegistry.GenerateProviderManifests(ctx, &cluster)
				Expect(err).To(BeNil())
				// Disable schedulable masters
				cluster.SchedulableMasters = swag.Bool(false)
				err = providerRegistry.GenerateProviderManifests(ctx, &cluster)
				Expect(err).To(BeNil())
			})
			It("3 masters - 2 workers ('SchedulableMasters' set)", func() {
				hosts := createHosts(3, 2, hostStatusKnown, platform)
				cluster := createClusterFromHostsPlatform(hosts, platform)
				cluster.SchedulableMasters = swag.Bool(true)
				err := providerRegistry.GenerateProviderManifests(ctx, &cluster)
				Expect(err).To(BeNil())
				// Enable schedulable masters
				cluster.SchedulableMasters = swag.Bool(true)
				err = providerRegistry.GenerateProviderManifests(ctx, &cluster)
				Expect(err).To(BeNil())
				// Disable schedulable masters
				cluster.SchedulableMasters = swag.Bool(false)
				err = providerRegistry.GenerateProviderManifests(ctx, &cluster)
				Expect(err).To(BeNil())
			})
		})
	}
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

func createHosts(masterNum, workerNum int, state string, platform models.PlatformType) []*models.Host {
	hosts := make([]*models.Host, 0)
	for masterNum > 0 {
		hostname := fmt.Sprintf("master-%d", masterNum)
		inventory := getPlatformInventoryStr(platform, hostname, "bios", true, false)
		hosts = append(hosts, createHost(true, state, inventory))
		masterNum--
	}
	for workerNum > 0 {
		hostname := fmt.Sprintf("worker-%d", workerNum)
		inventory := getPlatformInventoryStr(platform, hostname, "bios", true, false)
		hosts = append(hosts, createHost(false, state, inventory))
		workerNum--
	}

	return hosts
}

func getPlatformInventoryStr(platform models.PlatformType, hostname, bootMode string, ipv4, ipv6 bool) string {
	switch platform {
	case models.PlatformTypeOvirt:
		return getOvirtInventoryStr(hostname, bootMode, ipv4, ipv6)
	case models.PlatformTypeVsphere:
		return getVsphereInventoryStr(hostname, bootMode, ipv4, ipv6)
	case models.PlatformTypeBaremetal:
		return getBaremetalInventoryStr(hostname, bootMode, ipv4, ipv6)
	default:
		return ""
	}
}

func getInventory(hostname, bootMode string, ipv4, ipv6 bool) models.Inventory {
	inventory := models.Inventory{
		Hostname: hostname,
		Boot:     &models.Boot{CurrentBootMode: bootMode},
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: []string{},
				IPV6Addresses: []string{},
				MacAddress:    "some MAC address",
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
		ProductName:  "VMware7,1",
		SerialNumber: "VMware-12 34 56 78 90 12 ab cd-ef gh 12 34 56 67 89 90",
		Virtual:      true,
	}
	ret, _ := json.Marshal(&inventory)
	return string(ret)
}

func getOvirtInventoryStr(hostname, bootMode string, ipv4, ipv6 bool) string {
	inventory := getInventory(hostname, bootMode, ipv4, ipv6)
	inventory.SystemVendor = &models.SystemVendor{
		Manufacturer: "oVirt",
		ProductName:  "oVirt",
		SerialNumber: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
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

func createVspherePlatformParams() *models.Platform {
	return &models.Platform{
		Type: common.PlatformTypePtr(models.PlatformTypeVsphere),
	}
}

func createOvirtPlatformParams() *models.Platform {
	Password := strfmt.Password(ovirtPassword)
	ClusterID := strfmt.UUID(ovirtClusterID)
	StorageDomainID := strfmt.UUID(ovirtStorageDomainID)
	VnicProfileID := strfmt.UUID(ovirtVnicProfileID)

	ovirtPlatform := models.OvirtPlatform{
		Fqdn:            swag.String(ovirtFqdn),
		Insecure:        swag.Bool(ovirtInsecure),
		CaBundle:        swag.String(ovirtCaBundle),
		Username:        swag.String(ovirtUsername),
		Password:        &Password,
		ClusterID:       &ClusterID,
		StorageDomainID: &StorageDomainID,
		NetworkName:     swag.String(ovirtNetworkName),
		VnicProfileID:   &VnicProfileID,
	}
	return &models.Platform{
		Type:  common.PlatformTypePtr(models.PlatformTypeOvirt),
		Ovirt: &ovirtPlatform,
	}
}

func createClusterFromHosts(hosts []*models.Host) common.Cluster {
	var enabledHosts int64
	for _, host := range hosts {
		if *host.Status != models.HostStatusDisabled {
			enabledHosts++
		}
	}
	ID := strfmt.UUID(uuid.New().String())
	return common.Cluster{
		Cluster: models.Cluster{
			ID:               &ID,
			APIVip:           "192.168.10.10",
			Hosts:            hosts,
			IngressVip:       "192.168.10.11",
			OpenshiftVersion: "4.7",
			EnabledHostCount: enabledHosts,
		},
	}
}

func createClusterFromHostsPlatform(hosts []*models.Host, platform models.PlatformType) common.Cluster {
	cluster := createClusterFromHosts(hosts)
	switch platform {
	case models.PlatformTypeOvirt:
		cluster.Platform = createOvirtPlatformParams()
	case models.PlatformTypeVsphere:
		cluster.Platform = createVspherePlatformParams()
	case models.PlatformTypeBaremetal:
		cluster.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)}
	}
	return cluster
}

func getInstallerConfigBaremetal() installcfg.InstallerConfigBaremetal {
	return installcfg.InstallerConfigBaremetal{
		APIVersion: "v1",
		BaseDomain: "test.base.domain",
		Networking: struct {
			NetworkType    string                      `yaml:"networkType"`
			ClusterNetwork []installcfg.ClusterNetwork `yaml:"clusterNetwork"`
			MachineNetwork []installcfg.MachineNetwork `yaml:"machineNetwork,omitempty"`
			ServiceNetwork []string                    `yaml:"serviceNetwork"`
		}{
			NetworkType:    "OpenShiftSDN",
			ClusterNetwork: []installcfg.ClusterNetwork{{Cidr: "10.128.0.0/14", HostPrefix: 23}},
			MachineNetwork: []installcfg.MachineNetwork{{Cidr: "10.0.0.0/16"}},
			ServiceNetwork: []string{"172.30.0.0/16"},
		},
		Metadata: struct {
			Name string `yaml:"name"`
		}{Name: "dummy"},
		Compute: []struct {
			Hyperthreading string `yaml:"hyperthreading,omitempty"`
			Name           string `yaml:"name"`
			Replicas       int    `yaml:"replicas"`
		}{{
			Name:     "worker-test",
			Replicas: 2,
		}},
		ControlPlane: struct {
			Hyperthreading string `yaml:"hyperthreading,omitempty"`
			Name           string `yaml:"name"`
			Replicas       int    `yaml:"replicas"`
		}{
			Name:     "master-test",
			Replicas: 3,
		},
		Platform:              installcfg.Platform{},
		BootstrapInPlace:      installcfg.BootstrapInPlace{},
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
