package installcfg

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"gopkg.in/yaml.v2"
)

var _ = Describe("installcfg", func() {
	var (
		host1                             models.Host
		host2                             models.Host
		host3                             models.Host
		cluster                           common.Cluster
		ctrl                              *gomock.Controller
		mockMirrorRegistriesConfigBuilder *mirrorregistries.MockMirrorRegistriesConfigBuilder
		installConfig                     *installConfigBuilder
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clusterId := strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:                     &clusterId,
			OpenshiftVersion:       common.TestDefaultConfig.OpenShiftVersion,
			Name:                   "test-cluster",
			BaseDNSDomain:          "redhat.com",
			MachineNetworkCidr:     "1.2.3.0/24",
			APIVip:                 "102.345.34.34",
			IngressVip:             "376.5.56.6",
			InstallConfigOverrides: `{"fips":true}`,
			ImageInfo:              &models.ImageInfo{},
		}}
		id := strfmt.UUID(uuid.New().String())
		host1 = models.Host{
			ID:        &id,
			ClusterID: clusterId,
			Status:    swag.String(models.HostStatusKnown),
			Role:      "master",
			Inventory: getInventoryStr("hostname0", "bootMode", false),
		}
		id = strfmt.UUID(uuid.New().String())
		host2 = models.Host{
			ID:        &id,
			ClusterID: clusterId,
			Status:    swag.String(models.HostStatusKnown),
			Role:      "worker",
			Inventory: getInventoryStr("hostname1", "bootMode", false),
		}

		host3 = models.Host{
			ID:        &id,
			ClusterID: clusterId,
			Status:    swag.String(models.HostStatusKnown),
			Role:      "worker",
			Inventory: getInventoryStr("hostname2", "bootMode", false),
		}

		cluster.Hosts = []*models.Host{&host1, &host2, &host3}
		mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
		installConfig = &installConfigBuilder{log: common.GetTestLog(), mirrorRegistriesBuilder: mockMirrorRegistriesConfigBuilder}

	})

	It("create_configuration_with_all_hosts", func() {
		var result InstallerConfigBaremetal
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(result.Platform.Baremetal.Hosts)).Should(Equal(3))
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("create_configuration_with_hostnames", func() {
		var result InstallerConfigBaremetal
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(result.Platform.Baremetal.Hosts)).Should(Equal(3))
		Expect(result.Platform.Baremetal.Hosts[0].Name).Should(Equal("hostname0"))
		Expect(result.Platform.Baremetal.Hosts[1].Name).Should(Equal("hostname1"))
		Expect(result.Platform.Baremetal.Hosts[2].Name).Should(Equal("hostname2"))
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("create_configuration_with_one_host_disabled", func() {
		var result InstallerConfigBaremetal
		host3.Status = swag.String(models.HostStatusDisabled)
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(result.Platform.Baremetal.Hosts)).Should(Equal(2))
		Expect(result.Proxy).Should(BeNil())
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("create_configuration_with_mirror_registries", func() {
		var result InstallerConfigBaremetal
		regData := []mirrorregistries.RegistriesConf{{Location: "location1", Mirror: "mirror1"}, {Location: "location2", Mirror: "mirror2"}}
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(true).Times(2)
		mockMirrorRegistriesConfigBuilder.EXPECT().ExtractLocationMirrorDataFromRegistries().Return(regData, nil).Times(1)
		mockMirrorRegistriesConfigBuilder.EXPECT().GetMirrorCA().Return([]byte("some sa data"), nil).Times(1)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("create_configuration_with_proxy", func() {
		var result InstallerConfigBaremetal
		proxyURL := "http://proxyserver:3218"
		cluster.HTTPProxy = proxyURL
		cluster.HTTPSProxy = proxyURL
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Proxy.HTTPProxy).Should(Equal(proxyURL))
		Expect(result.Proxy.HTTPSProxy).Should(Equal(proxyURL))
		splitNoProxy := strings.Split(result.Proxy.NoProxy, ",")
		Expect(splitNoProxy).To(HaveLen(4))
		Expect(splitNoProxy).To(ContainElement(cluster.MachineNetworkCidr))
		Expect(splitNoProxy).To(ContainElement(cluster.ServiceNetworkCidr))
		Expect(splitNoProxy).To(ContainElement(cluster.ClusterNetworkCidr))
		domainName := "." + cluster.Name + "." + cluster.BaseDNSDomain
		Expect(splitNoProxy).To(ContainElement(domainName))
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("create_configuration_with_proxy_with_no_proxy", func() {
		var result InstallerConfigBaremetal
		proxyURL := "http://proxyserver:3218"
		cluster.HTTPProxy = proxyURL
		cluster.HTTPSProxy = proxyURL
		cluster.NoProxy = "no-proxy.com"
		cluster.MachineNetworkCidr = ""
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Proxy.HTTPProxy).Should(Equal(proxyURL))
		Expect(result.Proxy.HTTPSProxy).Should(Equal(proxyURL))
		splitNoProxy := strings.Split(result.Proxy.NoProxy, ",")
		Expect(splitNoProxy).To(HaveLen(4))
		Expect(splitNoProxy).To(ContainElement("no-proxy.com"))
		Expect(splitNoProxy).To(ContainElement(cluster.ServiceNetworkCidr))
		Expect(splitNoProxy).To(ContainElement(cluster.ClusterNetworkCidr))
		domainName := "." + cluster.Name + "." + cluster.BaseDNSDomain
		Expect(splitNoProxy).To(ContainElement(domainName))
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("correctly applies cluster overrides", func() {
		var result InstallerConfigBaremetal
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		// test that overrides worked
		Expect(result.FIPS).Should(Equal(true))
		// test that existing values are kept
		Expect(result.APIVersion).Should(Equal("v1"))
		Expect(result.BaseDomain).Should(Equal("redhat.com"))
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("doesn't fail with empty overrides", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("doesn't fail with empty overrides, IPv6 machine CIDR", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.MachineNetworkCidr = "1001:db8::/120"
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOVNKubernetes))
	})

	It("doesn't fail with empty overrides, IPv6 cluster CIDR", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.ClusterNetworkCidr = "1001:db8::/120"
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOVNKubernetes))
	})

	It("doesn't fail with empty overrides, IPv6 service CIDR", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.ServiceNetworkCidr = "1001:db8::/120"
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOVNKubernetes))
	})

	It("CA AdditionalTrustBundle", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		ca := "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
			"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
			"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, true, ca)
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.AdditionalTrustBundle).Should(Equal(" | -----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
			"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
			"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"))
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("CA AdditionalTrustBundle is set to mirror CA", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		ca := "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
			"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
			"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"
		mirrorCA := "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
			"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
			"2lyDI6UR3Fbz4pFDaxRgtF123FVTA=\n-----END CERTIFICATE-----"
		gomock.InOrder(mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false),
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(true))
		mockMirrorRegistriesConfigBuilder.EXPECT().GetMirrorCA().Return([]byte(mirrorCA), nil).Times(1)
		data, err := installConfig.GetInstallConfig(&cluster, true, ca)
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.AdditionalTrustBundle).Should(Equal(" | \n-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
			"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
			"2lyDI6UR3Fbz4pFDaxRgtF123FVTA=\n-----END CERTIFICATE-----"))
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("CA AdditionalTrustBundle not added", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "CA-CERT")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.AdditionalTrustBundle).Should(Equal(""))
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("UserManagedNetworking None Platform", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.UserManagedNetworking = swag.Bool(true)
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Platform.Baremetal).Should(BeNil())
		var none = platformNone{}
		Expect(*result.Platform.None).Should(Equal(none))
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("UserManagedNetworking None Platform Machine network", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.UserManagedNetworking = swag.Bool(true)
		cluster.MachineNetworkCidr = ""
		host1.Bootstrap = true
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Platform.Baremetal).Should(BeNil())
		var none = platformNone{}
		Expect(*result.Platform.None).Should(Equal(none))
		Expect(result.Networking.MachineNetwork[0].Cidr).Should(Equal("10.35.20.0/24"))
	})

	It("UserManagedNetworking None Platform Machine network IPV6 only", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.UserManagedNetworking = swag.Bool(true)
		cluster.MachineNetworkCidr = ""
		host1.Bootstrap = true
		host1.Inventory = getInventoryStr("hostname0", "bootMode", true)
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Platform.Baremetal).Should(BeNil())
		var none = platformNone{}
		Expect(*result.Platform.None).Should(Equal(none))
		Expect(result.Networking.MachineNetwork[0].Cidr).Should(Equal("fe80::/64"))
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOVNKubernetes))
	})

	It("UserManagedNetworking BareMetal", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.UserManagedNetworking = swag.Bool(false)
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Platform.Baremetal).ShouldNot(BeNil())
		Expect(result.Platform.None).Should(BeNil())
	})

	It("Single node", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.UserManagedNetworking = swag.Bool(true)
		mode := models.ClusterHighAvailabilityModeNone
		cluster.HighAvailabilityMode = &mode
		cluster.Hosts[0].Bootstrap = true
		cluster.Hosts[0].InstallationDiskPath = "/dev/test"
		cluster.MachineNetworkCidr = "10.35.20.0/24"
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)

		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Platform.Baremetal).Should(BeNil())
		Expect(*result.Platform.None).Should(Equal(platformNone{}))
		Expect(result.BootstrapInPlace.InstallationDisk).Should(Equal("/dev/test"))
		Expect(result.Networking.MachineNetwork[0].Cidr).Should(Equal(cluster.MachineNetworkCidr))
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOpenShiftSDN))
	})

	It("Single node IPV6 only", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.UserManagedNetworking = swag.Bool(true)
		cluster.MachineNetworkCidr = "fe80::/64"
		host1.Bootstrap = true
		host1.Inventory = getInventoryStr("hostname0", "bootMode", true)
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		data, err := installConfig.GetInstallConfig(&cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Platform.Baremetal).Should(BeNil())
		var none = platformNone{}
		Expect(*result.Platform.None).Should(Equal(none))
		Expect(result.Networking.MachineNetwork[0].Cidr).Should(Equal(cluster.MachineNetworkCidr))
		Expect(result.Networking.NetworkType).To(Equal(models.ClusterNetworkTypeOVNKubernetes))
	})

	It("Hyperthreading config", func() {
		cluster.Hyperthreading = "none"
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		data, err := installConfig.getBasicInstallConfig(&cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(data.ControlPlane.Hyperthreading).Should(Equal("Disabled"))
		Expect(data.Compute[0].Hyperthreading).Should(Equal("Disabled"))
		cluster.Hyperthreading = "all"
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		data, err = installConfig.getBasicInstallConfig(&cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(data.ControlPlane.Hyperthreading).Should(Equal("Enabled"))
		Expect(data.Compute[0].Hyperthreading).Should(Equal("Enabled"))
		cluster.Hyperthreading = "workers"
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		data, err = installConfig.getBasicInstallConfig(&cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(data.ControlPlane.Hyperthreading).Should(Equal("Disabled"))
		Expect(data.Compute[0].Hyperthreading).Should(Equal("Enabled"))
		cluster.Hyperthreading = "masters"
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		data, err = installConfig.getBasicInstallConfig(&cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(data.ControlPlane.Hyperthreading).Should(Equal("Enabled"))
		Expect(data.Compute[0].Hyperthreading).Should(Equal("Disabled"))
	})

	AfterEach(func() {
		// cleanup
		ctrl.Finish()
	})
})

var _ = Describe("ValidateInstallConfigPatch", func() {
	var (
		ctrl                              *gomock.Controller
		cluster                           *common.Cluster
		installConfigBuilder              InstallConfigBuilder
		mockMirrorRegistriesConfigBuilder *mirrorregistries.MockMirrorRegistriesConfigBuilder
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		id := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:               &id,
			OpenshiftVersion: "4.6",
			BaseDNSDomain:    "example.com",
			APIVip:           "102.345.34.34",
			IngressVip:       "376.5.56.6",
			ImageInfo:        &models.ImageInfo{},
		}}
		mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
		installConfigBuilder = NewInstallConfigBuilder(common.GetTestLog(), mockMirrorRegistriesConfigBuilder)
	})

	It("Succeeds when provided valid json", func() {
		s := `{"apiVersion": "v3", "baseDomain": "example.com", "metadata": {"name": "things"}}`
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		err := installConfigBuilder.ValidateInstallConfigPatch(cluster, s)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("Fails when provided invalid json", func() {
		s := `{"apiVersion": 3, "baseDomain": "example.com", "metadata": {"name": "things"}}`
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		err := installConfigBuilder.ValidateInstallConfigPatch(cluster, s)
		Expect(err).Should(HaveOccurred())
	})

	It("Fails with an invalid cert", func() {
		s := `{"additionalTrustBundle":  "-----BEGIN CERTIFICATE-----\nMIIFozCCA4ugAwIBAgIUVlT4eKQQ43HN31jQzsez+iEmpw8wDQYJKoZIhvcNAQEL\nBQAwYTELMAkGA1UEBhMCQUExFTATBgNVBAcMDERlZmF1bHQgQ2l0eTEcMBoGA1UE\nCgwTRGVmYXVsdCBDb21wYW55IEx0ZDEdMBsGA1UEAwwUcmVnaXN0cnkuZXhhbXBs\nZS5jb20wHhcNMjAxMDI3MTI0OTEwWhcNMjExMDI3MTI0OTEwWjBhMQswCQYDVQQG\nEwJBQTEVMBMGA1UEBwwMRGVmYXVsdCBDaXR5MRwwGgYDVQQKDBNEZWZhdWx0IENv\nbXBhbnkgTHRkMR0wGwYDVQQDDBRyZWdpc3RyeS5leGFtcGxlLmNvbTCCAiIwDQYJ\nKoZIhvcNAQEBBQADggIPADCCAgoCggIBAKm/wEl5B6lDOwYtkOxoLHQySA5RySEU\nkEMoGxBtGewLjLRMS9zp5pgYNcRenOTfUeyx6n4vE+lLn6p4laSig6QGDK0mmPl/\nt8OVZGBNE/dOZEoGe3I+gQux0oErhzjNxrf1EGfeBRVVuSqmgQnFaeLq2mGsbb5+\nyz114seD7u0Vb6OIX5sA+ytvr+jV3HK0jf5H9AHvSnNzF0UE+S7CHTJSDqQNUPxp\n8rAtfOvWyndDJBBmA0fdnDRYNtUqKcj/YBSntuZAmSJ0Woq9NrE+H3e61kvF0AP8\nHz21FSD/GqCn97Q8Mh8uTKx8jas2XBLyWdi0OCIV+a4jTadez1zPCWT+zgD5rHAk\np5RyXgkRU3guJydNMlpRPsGur3pUM4Q3zQfArZ+OxTkU/SLZbBmAVMPDI2pwL6qE\n2F8So4JdysH1MiwtYDYVIxKChrpBtTVunIe+Jyl/w8a3xR77r++3MFauobGLpeCL\nptbSz0aFZIIIwoLw2JVaWe7BWryjk8fDYrlPkLWqgQ956lcZppqiUzvEVv3p7wC2\nmfWkXJBGZZ0CZcYUoEE7zQ5T0RHLXqf0lSMf8I1SPzBF+Wl6G2gUOaZtYT5s0LA5\nid+gSDtKqyDH1HwPGO0eQB1LGeXOCLBA3cgmxYXtIMLfds0LgcJF+vRV3868abpD\n+yVMxGQRzRZFAgMBAAGjUzBRMB0GA1UdDgQWBBTUHUuivG1L6rTHS9v8KHTtOVpL\ncjAfBgNVHSMEGDAWgBTUHUuivG1L6rTHS9v8KHTtOVpLcjAPBgNVHRMBAf8EBTAD\nAQH/MA0GCSqGSIb3DQEBCwUAA4ICAQAFTmSriXnTJ/9cbO2lJmH7OFcrgKWdsycU\ngc9aeLSpnlYPuMjRHgXpq0X5iZzJaOXu8WKmbxTItxfd7MD/9rsaDMo7uDs6cZhC\nsdpWDVzZlP1PRcy1uT3+g12QmMmt89WBtauKEMukI3mOlx6y1VzPj9Vw5gfBKYjS\nh2NJPSVzgkLlLTOsY6bHesXVWrHVtCS5fUiE2xNkE6hXS0hZWYZlzLwn55wIrchx\nB3G++mPnNL3SbH62lXyWcrc1M/+gNl3F3jSd5WfxZQVllZ9vK1DnBKDisTUax5fR\nqK/D7vgkvHJa0USzGhcYV3DEdbgP/COgWrpbA0TTFcasWWYQdBk+2EUPcWKAh0JB\nVgql3o0pmyzfqQtuRRMC4D6Ip6y6IE2opK2c7ipXT4iEyPqr4uk4IeVFXghCYW92\nkCI+FyRJgbSu9ZuIug8AUlea7UOLTC4mxAayXvTwA6bNlGoSLmojgQHG7GlGj+E8\n57AHM2sD9Qi1VYyLuMVhJB3DzlQKtEFuvZsvi/rSIGqT8UfNbxk7OCtxceyzECqW\n2ptIv7tDhQeAGqkGqhTj1WdH+16+QZpsfmkwt5+hAaOeZfQ/nOCP7CGwbl4nYc3X\narDiqhVUXlv84/7XrOyoDJo3AVGidq902h6MYenX9T//XYbWkUK7nkvYMVoxu/Ek\nx/aT+8yOHQ==\n", "imageContentSources": [{"mirrors": ["registry.example.com:5000/ocp4"], "source": "quay.io/openshift-release-dev/ocp-release"}, {"mirrors": ["registry.example.com:5000/ocp4"], "source": "quay.io/openshift-release-dev/ocp-release-nightly"}, {"mirrors": ["registry.example.com:5000/ocp4"], "source": "quay.io/openshift-release-dev/ocp-v4.0-art-dev"}]}`
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		err := installConfigBuilder.ValidateInstallConfigPatch(cluster, s)
		Expect(err).Should(HaveOccurred())
	})

	It("Single node - valid json without bootstrap node", func() {
		s := `{"apiVersion": "v3", "baseDomain": "example.com", "metadata": {"name": "things"}}`
		cluster.UserManagedNetworking = swag.Bool(true)
		mode := models.ClusterHighAvailabilityModeNone
		cluster.HighAvailabilityMode = &mode
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
		err := installConfigBuilder.ValidateInstallConfigPatch(cluster, s)
		Expect(err).ShouldNot(HaveOccurred())
	})
})

func getInventoryStr(hostname, bootMode string, ipv6only bool) string {
	inventory := models.Inventory{
		Hostname: hostname,
		Boot:     &models.Boot{CurrentBootMode: bootMode},
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: append(make([]string, 0), "10.35.20.10/24"),
				IPV6Addresses: append(make([]string, 0), "fe80::1/64"),
				MacAddress:    "some MAC address",
			},
		},
	}
	if ipv6only {
		inventory.Interfaces[0].IPV4Addresses = nil
	}
	ret, _ := json.Marshal(&inventory)
	return string(ret)
}

var _ = Describe("Generate NoProxy", func() {
	var (
		ctrl                              *gomock.Controller
		cluster                           *common.Cluster
		mockMirrorRegistriesConfigBuilder *mirrorregistries.MockMirrorRegistriesConfigBuilder
		installConfig                     *installConfigBuilder
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		cluster = &common.Cluster{Cluster: models.Cluster{
			MachineNetworkCidr: "10.35.20.0/24",
			Name:               "proxycluster",
			BaseDNSDomain:      "myproxy.com",
			ClusterNetworkCidr: "192.168.1.0/24",
			ServiceNetworkCidr: "fe80::1/64",
		}}
		mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
		installConfig = &installConfigBuilder{log: common.GetTestLog(), mirrorRegistriesBuilder: mockMirrorRegistriesConfigBuilder}
	})
	It("Default NoProxy", func() {
		noProxy := installConfig.generateNoProxy(cluster)
		Expect(noProxy).Should(Equal("10.35.20.0/24,.proxycluster.myproxy.com,192.168.1.0/24,fe80::1/64"))
	})
	It("Update NoProxy", func() {
		cluster.NoProxy = "domain.org,127.0.0.2"
		noProxy := installConfig.generateNoProxy(cluster)
		Expect(noProxy).Should(Equal("domain.org,127.0.0.2,10.35.20.0/24,.proxycluster.myproxy.com,192.168.1.0/24,fe80::1/64"))
	})
	It("All-excluded NoProxy", func() {
		cluster.NoProxy = "*"
		noProxy := installConfig.generateNoProxy(cluster)
		Expect(noProxy).Should(Equal("*"))
	})
	It("All-excluded NoProxy with spaces", func() {
		cluster.NoProxy = " * "
		noProxy := installConfig.generateNoProxy(cluster)
		Expect(noProxy).Should(Equal("*"))
	})
})

var _ = Describe("getNetworkType", func() {
	tests := []struct {
		inNetworkType      string
		serviceNetworkCidr string
		outNetworkType     string
	}{
		{
			inNetworkType:      models.ClusterCreateParamsNetworkTypeAutoAssign,
			serviceNetworkCidr: "1.2.8.0/23",
			outNetworkType:     models.ClusterNetworkTypeOpenShiftSDN,
		},
		{
			inNetworkType:      models.ClusterCreateParamsNetworkTypeAutoAssign,
			serviceNetworkCidr: "1002:db8::/119",
			outNetworkType:     models.ClusterNetworkTypeOVNKubernetes,
		},
		{
			inNetworkType:      models.ClusterNetworkTypeOpenShiftSDN,
			serviceNetworkCidr: "1.2.8.0/23",
			outNetworkType:     models.ClusterNetworkTypeOpenShiftSDN,
		},
		{
			inNetworkType:      models.ClusterNetworkTypeOVNKubernetes,
			serviceNetworkCidr: "1002:db8::/119",
			outNetworkType:     models.ClusterNetworkTypeOVNKubernetes,
		},
		{
			inNetworkType:      models.ClusterNetworkTypeOVNKubernetes,
			serviceNetworkCidr: "1.2.8.0/23",
			outNetworkType:     models.ClusterNetworkTypeOVNKubernetes,
		},
	}
	for i := range tests {
		t := tests[i]
		It("getNetworkType", func() {
			ctrl := gomock.NewController(GinkgoT())
			clusterId := strfmt.UUID(uuid.New().String())
			cluster := &common.Cluster{Cluster: models.Cluster{
				NetworkType:        &t.inNetworkType,
				ID:                 &clusterId,
				ServiceNetworkCidr: t.serviceNetworkCidr,
				ClusterNetworkCidr: "10.56.20.0/24",
				MachineNetworkCidr: "1.2.3.0/24",
			}}
			mockMirrorRegistriesConfigBuilder := mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
			installConfig := &installConfigBuilder{log: common.GetTestLog(), mirrorRegistriesBuilder: mockMirrorRegistriesConfigBuilder}
			networkType := installConfig.getNetworkType(cluster)
			Expect(networkType).To(Equal(t.outNetworkType))
		})
	}
})

func TestSubsystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "installcfg tests")
}
