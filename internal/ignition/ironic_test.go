package ignition

import (
	"net/url"

	config_32 "github.com/coreos/ignition/v2/config/v3_2"
	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/vincent-petithory/dataurl"
)

var _ = Describe("GenerateIronicConfig", func() {
	var (
		infraEnv      common.InfraEnv
		infraEnvID    strfmt.UUID
		ironicBaseURL = "https://10.10.10.10"
		inspectorURL  = "https://10.10.10.11"
	)
	BeforeEach(func() {
		infraEnvID = "a64fff36-dcb1-11ea-87d0-0242ac130003"
		infraEnv = common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:            &infraEnvID,
				PullSecretSet: false,
			},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
	})
	It("GenerateIronicConfig override default ironic agent image", func() {
		ironicAgentImage := "ironicAgentImage:custom"
		conf, err := GenerateIronicConfig(ironicBaseURL, inspectorURL, infraEnv, ironicAgentImage)
		Expect(err).NotTo(HaveOccurred())
		Expect(conf).Should(ContainSubstring("ironic-agent.service"))
		Expect(conf).Should(ContainSubstring(url.QueryEscape(ironicBaseURL)))
		Expect(conf).Should(ContainSubstring(ironicAgentImage))
		validateIgnition(conf)
	})
	It("GenerateIronicConfig missing ironic service URL", func() {
		_, err := GenerateIronicConfig("", "", infraEnv, "")
		Expect(err).To(HaveOccurred())
	})
	It("GenerateIronicConfig missing ironic agent image", func() {
		_, err := GenerateIronicConfig(ironicBaseURL, inspectorURL, infraEnv, "")
		Expect(err).To(HaveOccurred())
	})
	It("set the ironic inspector config to not tag interfaces when static networking is configured", func() {
		infraEnv.StaticNetworkConfig = "some network config here"
		conf, err := GenerateIronicConfig(ironicBaseURL, inspectorURL, infraEnv, "ironicAgentImage:custom")
		Expect(err).NotTo(HaveOccurred())
		Expect(conf).Should(ContainSubstring(dataurl.Escape([]byte("enable_vlan_interfaces = \n"))))
	})
	It("sets the ironic inspector config to tag all interfaces when static networking is not configured", func() {
		conf, err := GenerateIronicConfig(ironicBaseURL, inspectorURL, infraEnv, "ironicAgentImage:custom")
		Expect(err).NotTo(HaveOccurred())
		Expect(conf).Should(ContainSubstring(dataurl.Escape([]byte("enable_vlan_interfaces = all\n"))))
	})
})

func validateIgnition(conf []byte) {
	v32Config, _, err1 := config_32.Parse(conf)
	Expect(err1).ToNot(HaveOccurred())
	Expect(v32Config.Ignition.Version).To(Equal("3.2.0"))
	// We expect a single file and a single systemd unit
	Expect(len(v32Config.Storage.Files)).To(Equal(1))
	Expect(len(v32Config.Systemd.Units)).To(Equal(1))
}
