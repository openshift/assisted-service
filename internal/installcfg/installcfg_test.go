package installcfg

import (
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
			OpenshiftVersion:       "4.5",
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
		}
		id = strfmt.UUID(uuid.New().String())
		host2 = models.Host{
			ID:        &id,
			ClusterID: clusterId,
			Status:    swag.String(models.HostStatusKnown),
			Role:      "worker",
		}

		host3 = models.Host{
			ID:        &id,
			ClusterID: clusterId,
			Status:    swag.String(models.HostStatusKnown),
			Role:      "worker",
		}

		cluster.Hosts = []*models.Host{&host1, &host2, &host3}
		ctrl = gomock.NewController(GinkgoT())

	})

	It("create_configuration_with_all_hosts", func() {
		var result InstallerConfigBaremetal
		data, err := GetInstallConfig(logrus.New(), &cluster)
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(result.Platform.Baremetal.Hosts)).Should(Equal(3))
	})

	It("create_configuration_with_one_host_disabled", func() {
		var result InstallerConfigBaremetal
		host3.Status = swag.String(models.HostStatusDisabled)
		data, err := GetInstallConfig(logrus.New(), &cluster)
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
		data, err := GetInstallConfig(logrus.New(), &cluster)
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Proxy.HTTPProxy).Should(Equal(proxyURL))
		Expect(result.Proxy.HTTPSProxy).Should(Equal(proxyURL))
	})

	It("correctly applies cluster overrides", func() {
		var result InstallerConfigBaremetal
		data, err := GetInstallConfig(logrus.New(), &cluster)
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
		data, err := GetInstallConfig(logrus.New(), &cluster)
		Expect(err).ShouldNot(HaveOccurred())
		err = yaml.Unmarshal(data, &result)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result.Networking.NetworkType).Should(Equal("OpenShiftSDN"))
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

func TestSubsystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "installcfg tests")
}
