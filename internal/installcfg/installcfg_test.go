package installcfg

import (
	"encoding/json"
	"testing"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("installcfg", func() {
	var (
		host1   models.Host
		host2   models.Host
		host3   models.Host
		cluster common.Cluster
		ctrl    *gomock.Controller
	)
	BeforeEach(func() {
		clusterId := strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:                     &clusterId,
			OpenshiftVersion:       common.DefaultTestOpenShiftVersion,
			BaseDNSDomain:          "redhat.com",
			APIVip:                 "102.345.34.34",
			IngressVip:             "376.5.56.6",
			InstallConfigOverrides: `{"networking":{"networkType": "OVN-Kubernetes"},"fips":true}`,
		}}
		id := strfmt.UUID(uuid.New().String())
		host1 = models.Host{
			ID:        &id,
			ClusterID: clusterId,
			Status:    swag.String(models.HostStatusKnown),
			Role:      "master",
			Inventory: getInventoryStr("hostname0", "bootMode"),
		}
		id = strfmt.UUID(uuid.New().String())
		host2 = models.Host{
			ID:        &id,
			ClusterID: clusterId,
			Status:    swag.String(models.HostStatusKnown),
			Role:      "worker",
			Inventory: getInventoryStr("hostname1", "bootMode"),
		}

		host3 = models.Host{
			ID:        &id,
			ClusterID: clusterId,
			Status:    swag.String(models.HostStatusKnown),
			Role:      "worker",
			Inventory: getInventoryStr("hostname2", "bootMode"),
		}

		cluster.Hosts = []*models.Host{&host1, &host2, &host3}
		ctrl = gomock.NewController(GinkgoT())

	})

	It("create_configuration_with_all_hosts", func() {
		var result InstallerConfigBaremetal
		data, err := GetInstallConfig(logrus.New(), &cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(result.Platform.Baremetal.Hosts)).Should(Equal(3))
	})

	It("create_configuration_with_one_host_disabled", func() {
		var result InstallerConfigBaremetal
		host3.Status = swag.String(models.HostStatusDisabled)
		data, err := GetInstallConfig(logrus.New(), &cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(result.Platform.Baremetal.Hosts)).Should(Equal(2))
		Expect(result.Proxy).Should(BeNil())
	})

	It("create_configuration_with_proxy", func() {
		var result InstallerConfigBaremetal
		proxyURL := "http://proxyserver:3218"
		cluster.HTTPProxy = proxyURL
		cluster.HTTPSProxy = proxyURL
		data, err := GetInstallConfig(logrus.New(), &cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Proxy.HTTPProxy).Should(Equal(proxyURL))
		Expect(result.Proxy.HTTPSProxy).Should(Equal(proxyURL))
	})

	It("correctly applies cluster overrides", func() {
		var result InstallerConfigBaremetal
		data, err := GetInstallConfig(logrus.New(), &cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		// test that overrides worked
		Expect(result.Networking.NetworkType).Should(Equal("OVN-Kubernetes"))
		Expect(result.FIPS).Should(Equal(true))
		// test that existing values are kept
		Expect(result.APIVersion).Should(Equal("v1"))
		Expect(result.BaseDomain).Should(Equal("redhat.com"))
	})

	It("doesn't fail with empty overrides", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		data, err := GetInstallConfig(logrus.New(), &cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Networking.NetworkType).Should(Equal("OpenShiftSDN"))
	})

	It("CA AdditionalTrustBundle", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		ca := "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
			"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
			"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"
		data, err := GetInstallConfig(logrus.New(), &cluster, true, ca)
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.AdditionalTrustBundle).Should(Equal(" | -----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
			"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
			"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"))
	})

	It("CA AdditionalTrustBundle not added", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		data, err := GetInstallConfig(logrus.New(), &cluster, false, "CA-CERT")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.AdditionalTrustBundle).Should(Equal(""))
	})

	It("UserManagedNetworking None Platform", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.UserManagedNetworking = swag.Bool(true)
		data, err := GetInstallConfig(logrus.New(), &cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Platform.Baremetal).Should(BeNil())
		var none = platformNone{}
		Expect(*result.Platform.None).Should(Equal(none))
	})

	It("UserManagedNetworking BareMetal", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.UserManagedNetworking = swag.Bool(false)
		data, err := GetInstallConfig(logrus.New(), &cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Platform.Baremetal).ShouldNot(BeNil())
		Expect(result.Platform.None).Should(BeNil())
	})

	AfterEach(func() {
		// cleanup
		ctrl.Finish()
	})
})

var _ = Describe("ValidateInstallConfigJSON", func() {
	It("Succeeds when provided valid json", func() {
		s := `{"apiVersion": "v3", "baseDomain": "example.com", "metadata": {"name": "things"}}`
		err := ValidateInstallConfigJSON(s)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("Fails when provided invalid json", func() {
		s := `{"apiVersion": 3, "baseDomain": "example.com", "metadata": {"name": "things"}}`
		err := ValidateInstallConfigJSON(s)
		Expect(err).Should(HaveOccurred())
	})
})

func getInventoryStr(hostname, bootMode string) string {
	inventory := models.Inventory{
		Hostname: hostname,
		Boot:     &models.Boot{CurrentBootMode: bootMode},
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: append(make([]string, 0), "some ip address"),
				MacAddress:    "some MAC address",
			},
		},
	}
	ret, _ := json.Marshal(&inventory)
	return string(ret)
}

func TestSubsystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "installcfg tests")
}
