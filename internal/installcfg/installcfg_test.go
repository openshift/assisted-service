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

const OvnKubernetes = "OVNKubernetes"

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
			OpenshiftVersion:       common.TestDefaultConfig.OpenShiftVersion,
			BaseDNSDomain:          "redhat.com",
			APIVip:                 "102.345.34.34",
			IngressVip:             "376.5.56.6",
			InstallConfigOverrides: `{"networking":{"networkType": "OVNKubernetes"},"fips":true}`,
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
		Expect(result.Networking.NetworkType).Should(Equal(OvnKubernetes))
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

	It("doesn't fail with empty overrides, IPv6 machine CIDR", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.MachineNetworkCidr = "1001:db8::/120"
		data, err := GetInstallConfig(logrus.New(), &cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Networking.NetworkType).Should(Equal(OvnKubernetes))
	})

	It("doesn't fail with empty overrides, IPv6 cluster CIDR", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.ClusterNetworkCidr = "1001:db8::/120"
		data, err := GetInstallConfig(logrus.New(), &cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Networking.NetworkType).Should(Equal(OvnKubernetes))
	})

	It("doesn't fail with empty overrides, IPv6 service CIDR", func() {
		var result InstallerConfigBaremetal
		cluster.InstallConfigOverrides = ""
		cluster.ServiceNetworkCidr = "1001:db8::/120"
		data, err := GetInstallConfig(logrus.New(), &cluster, false, "")
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Networking.NetworkType).Should(Equal(OvnKubernetes))
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

var _ = Describe("ValidateInstallConfigPatch", func() {
	var (
		cluster *common.Cluster
	)
	BeforeEach(func() {
		id := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:               &id,
			OpenshiftVersion: "4.6",
			BaseDNSDomain:    "example.com",
			APIVip:           "102.345.34.34",
			IngressVip:       "376.5.56.6",
		}}
	})

	It("Succeeds when provided valid json", func() {
		s := `{"apiVersion": "v3", "baseDomain": "example.com", "metadata": {"name": "things"}}`
		err := ValidateInstallConfigPatch(logrus.New(), cluster, s)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("Fails when provided invalid json", func() {
		s := `{"apiVersion": 3, "baseDomain": "example.com", "metadata": {"name": "things"}}`
		err := ValidateInstallConfigPatch(logrus.New(), cluster, s)
		Expect(err).Should(HaveOccurred())
	})

	It("Fails with an invalid cert", func() {
		s := `{"additionalTrustBundle":  "-----BEGIN CERTIFICATE-----\nMIIFozCCA4ugAwIBAgIUVlT4eKQQ43HN31jQzsez+iEmpw8wDQYJKoZIhvcNAQEL\nBQAwYTELMAkGA1UEBhMCQUExFTATBgNVBAcMDERlZmF1bHQgQ2l0eTEcMBoGA1UE\nCgwTRGVmYXVsdCBDb21wYW55IEx0ZDEdMBsGA1UEAwwUcmVnaXN0cnkuZXhhbXBs\nZS5jb20wHhcNMjAxMDI3MTI0OTEwWhcNMjExMDI3MTI0OTEwWjBhMQswCQYDVQQG\nEwJBQTEVMBMGA1UEBwwMRGVmYXVsdCBDaXR5MRwwGgYDVQQKDBNEZWZhdWx0IENv\nbXBhbnkgTHRkMR0wGwYDVQQDDBRyZWdpc3RyeS5leGFtcGxlLmNvbTCCAiIwDQYJ\nKoZIhvcNAQEBBQADggIPADCCAgoCggIBAKm/wEl5B6lDOwYtkOxoLHQySA5RySEU\nkEMoGxBtGewLjLRMS9zp5pgYNcRenOTfUeyx6n4vE+lLn6p4laSig6QGDK0mmPl/\nt8OVZGBNE/dOZEoGe3I+gQux0oErhzjNxrf1EGfeBRVVuSqmgQnFaeLq2mGsbb5+\nyz114seD7u0Vb6OIX5sA+ytvr+jV3HK0jf5H9AHvSnNzF0UE+S7CHTJSDqQNUPxp\n8rAtfOvWyndDJBBmA0fdnDRYNtUqKcj/YBSntuZAmSJ0Woq9NrE+H3e61kvF0AP8\nHz21FSD/GqCn97Q8Mh8uTKx8jas2XBLyWdi0OCIV+a4jTadez1zPCWT+zgD5rHAk\np5RyXgkRU3guJydNMlpRPsGur3pUM4Q3zQfArZ+OxTkU/SLZbBmAVMPDI2pwL6qE\n2F8So4JdysH1MiwtYDYVIxKChrpBtTVunIe+Jyl/w8a3xR77r++3MFauobGLpeCL\nptbSz0aFZIIIwoLw2JVaWe7BWryjk8fDYrlPkLWqgQ956lcZppqiUzvEVv3p7wC2\nmfWkXJBGZZ0CZcYUoEE7zQ5T0RHLXqf0lSMf8I1SPzBF+Wl6G2gUOaZtYT5s0LA5\nid+gSDtKqyDH1HwPGO0eQB1LGeXOCLBA3cgmxYXtIMLfds0LgcJF+vRV3868abpD\n+yVMxGQRzRZFAgMBAAGjUzBRMB0GA1UdDgQWBBTUHUuivG1L6rTHS9v8KHTtOVpL\ncjAfBgNVHSMEGDAWgBTUHUuivG1L6rTHS9v8KHTtOVpLcjAPBgNVHRMBAf8EBTAD\nAQH/MA0GCSqGSIb3DQEBCwUAA4ICAQAFTmSriXnTJ/9cbO2lJmH7OFcrgKWdsycU\ngc9aeLSpnlYPuMjRHgXpq0X5iZzJaOXu8WKmbxTItxfd7MD/9rsaDMo7uDs6cZhC\nsdpWDVzZlP1PRcy1uT3+g12QmMmt89WBtauKEMukI3mOlx6y1VzPj9Vw5gfBKYjS\nh2NJPSVzgkLlLTOsY6bHesXVWrHVtCS5fUiE2xNkE6hXS0hZWYZlzLwn55wIrchx\nB3G++mPnNL3SbH62lXyWcrc1M/+gNl3F3jSd5WfxZQVllZ9vK1DnBKDisTUax5fR\nqK/D7vgkvHJa0USzGhcYV3DEdbgP/COgWrpbA0TTFcasWWYQdBk+2EUPcWKAh0JB\nVgql3o0pmyzfqQtuRRMC4D6Ip6y6IE2opK2c7ipXT4iEyPqr4uk4IeVFXghCYW92\nkCI+FyRJgbSu9ZuIug8AUlea7UOLTC4mxAayXvTwA6bNlGoSLmojgQHG7GlGj+E8\n57AHM2sD9Qi1VYyLuMVhJB3DzlQKtEFuvZsvi/rSIGqT8UfNbxk7OCtxceyzECqW\n2ptIv7tDhQeAGqkGqhTj1WdH+16+QZpsfmkwt5+hAaOeZfQ/nOCP7CGwbl4nYc3X\narDiqhVUXlv84/7XrOyoDJo3AVGidq902h6MYenX9T//XYbWkUK7nkvYMVoxu/Ek\nx/aT+8yOHQ==\n", "imageContentSources": [{"mirrors": ["registry.example.com:5000/ocp4"], "source": "quay.io/openshift-release-dev/ocp-release"}, {"mirrors": ["registry.example.com:5000/ocp4"], "source": "quay.io/openshift-release-dev/ocp-release-nightly"}, {"mirrors": ["registry.example.com:5000/ocp4"], "source": "quay.io/openshift-release-dev/ocp-v4.0-art-dev"}]}`
		err := ValidateInstallConfigPatch(logrus.New(), cluster, s)
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
