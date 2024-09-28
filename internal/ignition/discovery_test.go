package ignition

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	config_31 "github.com/coreos/ignition/v2/config/v3_1"
	types_31 "github.com/coreos/ignition/v2/config/v3_1/types"
	config_32 "github.com/coreos/ignition/v2/config/v3_2"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vincent-petithory/dataurl"
)

var _ = Describe("proxySettingsForIgnition", func() {

	Context("test proxy settings in discovery ignition", func() {
		var parameters = []struct {
			httpProxy, httpsProxy, noProxy, res string
		}{
			{"", "", "", ""},
			{
				"http://proxy.proxy", "", "",
				`"proxy": { "httpProxy": "http://proxy.proxy" }`,
			},
			{
				"http://proxy.proxy", "https://proxy.proxy", "",
				`"proxy": { "httpProxy": "http://proxy.proxy", "httpsProxy": "https://proxy.proxy" }`,
			},
			{
				"http://proxy.proxy", "", ".domain",
				`"proxy": { "httpProxy": "http://proxy.proxy", "noProxy": [".domain"] }`,
			},
			{
				"http://proxy.proxy", "https://proxy.proxy", ".domain",
				`"proxy": { "httpProxy": "http://proxy.proxy", "httpsProxy": "https://proxy.proxy", "noProxy": [".domain"] }`,
			},
			{
				"", "https://proxy.proxy", ".domain,123.123.123.123",
				`"proxy": { "httpsProxy": "https://proxy.proxy", "noProxy": [".domain","123.123.123.123"] }`,
			},
			{
				"", "https://proxy.proxy", "",
				`"proxy": { "httpsProxy": "https://proxy.proxy" }`,
			},
			{
				"", "", ".domain", "",
			},
		}

		It("verify rendered proxy settings", func() {
			for _, p := range parameters {
				s, err := proxySettingsForIgnition(p.httpProxy, p.httpsProxy, p.noProxy)
				Expect(err).To(BeNil())
				Expect(s).To(Equal(p.res))
			}
		})
	})
})

var _ = Describe("IgnitionBuilder", func() {
	var (
		ctrl                              *gomock.Controller
		cluster                           *common.Cluster
		infraEnv                          common.InfraEnv
		log                               logrus.FieldLogger
		builder                           IgnitionBuilder
		mockStaticNetworkConfig           *staticnetworkconfig.MockStaticNetworkConfig
		mockMirrorRegistriesConfigBuilder *mirrorregistries.MockMirrorRegistriesConfigBuilder
		infraEnvID                        strfmt.UUID
		mockOcRelease                     *oc.MockRelease
		mockVersionHandler                *versions.MockHandler
		ignitionConfig                    IgnitionConfig
	)

	BeforeEach(func() {
		ignitionConfig = IgnitionConfig{EnableOKDSupport: true}
		log = common.GetTestLog()
		infraEnvID = strfmt.UUID("a640ef36-dcb1-11ea-87d0-0242ac130003")
		ctrl = gomock.NewController(GinkgoT())
		mockStaticNetworkConfig = staticnetworkconfig.NewMockStaticNetworkConfig(ctrl)
		mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
		mockOcRelease = oc.NewMockRelease(ctrl)
		mockVersionHandler = versions.NewMockHandler(ctrl)
		infraEnv = common.InfraEnv{InfraEnv: models.InfraEnv{
			ID:            &infraEnvID,
			PullSecretSet: false,
		}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}
		clusterID := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
			Cluster: models.Cluster{
				ID: &clusterID,
				MachineNetworks: []*models.MachineNetwork{{
					Cidr: "192.168.126.11/24",
				}},
			},
		}
		cluster.ImageInfo = &models.ImageInfo{}
		var err error
		builder, err = NewBuilder(log, mockStaticNetworkConfig, mockMirrorRegistriesConfigBuilder, mockOcRelease, mockVersionHandler)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("with auth enabled", func() {

		It("ignition_file_fails_missing_Pull_Secret_token", func() {
			infraEnvID = strfmt.UUID("a640ef36-dcb1-11ea-87d0-0242ac130003")
			infraEnvWithoutToken := common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:            &infraEnvID,
				PullSecretSet: false,
			}, PullSecret: "{\"auths\":{\"registry.redhat.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}
			_, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnvWithoutToken, ignitionConfig, false, auth.TypeRHSSO, "")

			Expect(err).ShouldNot(BeNil())
		})

		It("ignition_file_contains_pull_secret_token", func() {
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")

			Expect(err).Should(BeNil())
			Expect(text).Should(ContainSubstring("PULL_SECRET_TOKEN"))
		})

		It("ignition_file_contains_additoinal_trust_bundle", func() {
			const magicString string = "somemagicstring"

			// Try with bundle
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(2)
			infraEnv.AdditionalTrustBundle = magicString
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
			Expect(err).Should(BeNil())
			Expect(text).Should(ContainSubstring(dataurl.EncodeBytes([]byte(magicString))))

			// Try also without bundle
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			infraEnv.AdditionalTrustBundle = ""
			text, err = builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
			Expect(err).Should(BeNil())
			Expect(text).ShouldNot(ContainSubstring(dataurl.EncodeBytes([]byte(magicString))))
		})
	})

	It("auth_disabled_no_pull_secret_token", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeNone, "")

		Expect(err).Should(BeNil())
		Expect(text).ShouldNot(ContainSubstring("PULL_SECRET_TOKEN"))
	})

	It("ignition_file_contains_url", func() {
		serviceBaseURL := "file://10.56.20.70:7878"
		ignitionConfig.ServiceBaseURL = serviceBaseURL
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring(fmt.Sprintf("--url %s", serviceBaseURL)))
	})

	It("ignition_file_safe_for_logging", func() {
		serviceBaseURL := "file://10.56.20.70:7878"
		ignitionConfig.ServiceBaseURL = serviceBaseURL
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, true, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).ShouldNot(ContainSubstring("cloud.openshift.com"))
		Expect(text).Should(ContainSubstring("data:,*****"))
	})

	It("enabled_cert_verification", func() {
		ignitionConfig.SkipCertVerification = false
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring("--insecure=false"))
	})

	It("disabled_cert_verification", func() {
		ignitionConfig.SkipCertVerification = true
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring("--insecure=true"))
	})

	It("cert_verification_enabled_by_default", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring("--insecure=false"))
	})

	DescribeTable("ignition_file_contains_http_proxy",
		func(proxy models.Proxy, expectedIgnitionProxySetting, expectedProxyScriptSetting string, expectedAgentSeriveProxySetting string) {
			infraEnv.Proxy = &proxy
			serviceBaseURL := "file://10.56.20.70:7878"
			ignitionConfig.ServiceBaseURL = serviceBaseURL
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error"))

			By("verify ignition file contains only the proxy config entries that are set")
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
			Expect(err).Should(BeNil())
			Expect(text).Should(ContainSubstring(expectedIgnitionProxySetting))

			By("verify proxy.sh is correctly generated")
			minifiedText := strings.ReplaceAll(strings.ReplaceAll(text, " ", ""), "\n", "")
			expectedFileContent := dataurl.EncodeBytes([]byte(expectedProxyScriptSetting))
			Expect(minifiedText).Should(ContainSubstring(`{"path":"/etc/profile.d/proxy.sh","mode":644,"user":{"name":"root"},"contents":{"source":"` + expectedFileContent + `"}}`))

			By("verify agent.service proxy settings are correct")
			Expect(minifiedText).Should(ContainSubstring(expectedAgentSeriveProxySetting))
		},
		Entry(
			"http",
			models.Proxy{HTTPProxy: swag.String("http://10.10.1.1:3128"), NoProxy: swag.String("quay.io")},
			`"proxy": { "httpProxy": "http://10.10.1.1:3128", "noProxy": ["quay.io"] }`,
			fmt.Sprintf(
				"export HTTP_PROXY=%[1]s\nexport http_proxy=%[1]s\nexport NO_PROXY=%[2]s\nexport no_proxy=%[2]s\n",
				"http://10.10.1.1:3128",
				"quay.io",
			),
			fmt.Sprintf(
				`Environment=HTTP_PROXY=%[1]s\nEnvironment=http_proxy=%[1]s\nEnvironment=HTTPS_PROXY=\nEnvironment=https_proxy=\nEnvironment=NO_PROXY=%[2]s\nEnvironment=no_proxy=%[2]s`,
				"http://10.10.1.1:3128",
				"quay.io",
			),
		),
		Entry(
			"https",
			models.Proxy{HTTPSProxy: swag.String("https://10.10.1.1:3128")},
			`"proxy": { "httpsProxy": "https://10.10.1.1:3128"`,
			fmt.Sprintf(
				"export HTTPS_PROXY=%[1]s\nexport https_proxy=%[1]s\n",
				"https://10.10.1.1:3128",
			),
			fmt.Sprintf(
				`Environment=HTTP_PROXY=\nEnvironment=http_proxy=\nEnvironment=HTTPS_PROXY=%[1]s\nEnvironment=https_proxy=%[1]s\nEnvironment=NO_PROXY=\nEnvironment=no_proxy=`,
				"https://10.10.1.1:3128",
			),
		),
		Entry(
			"http with special characters",
			models.Proxy{HTTPProxy: swag.String("http://usr%40name:passwd%5D@10.10.1.1:3128"), NoProxy: swag.String("quay.io")},
			`"proxy": { "httpProxy": "http://usr%40name:passwd%5D@10.10.1.1:3128", "noProxy": ["quay.io"] }`,
			fmt.Sprintf(
				"export HTTP_PROXY=%[1]s\nexport http_proxy=%[1]s\nexport NO_PROXY=%[2]s\nexport no_proxy=%[2]s\n",
				"http://usr%40name:passwd%5D@10.10.1.1:3128",
				"quay.io",
			),
			fmt.Sprintf(
				`Environment=HTTP_PROXY=%[1]s\nEnvironment=http_proxy=%[1]s\nEnvironment=HTTPS_PROXY=\nEnvironment=https_proxy=\nEnvironment=NO_PROXY=%[2]s\nEnvironment=no_proxy=%[2]s`,
				"http://usr%%40name:passwd%%5D@10.10.1.1:3128",
				"quay.io",
			),
		),
		Entry(
			"https with special characters",
			models.Proxy{HTTPSProxy: swag.String("https://usr%40name:passwd%5D@10.10.1.1:3128")},
			`"proxy": { "httpsProxy": "https://usr%40name:passwd%5D@10.10.1.1:3128"`,
			fmt.Sprintf(
				"export HTTPS_PROXY=%[1]s\nexport https_proxy=%[1]s\n",
				"https://usr%40name:passwd%5D@10.10.1.1:3128",
			),
			fmt.Sprintf(
				`Environment=HTTP_PROXY=\nEnvironment=http_proxy=\nEnvironment=HTTPS_PROXY=%[1]s\nEnvironment=https_proxy=%[1]s\nEnvironment=NO_PROXY=\nEnvironment=no_proxy=`,
				"https://usr%%40name:passwd%%5D@10.10.1.1:3128",
			),
		),
		Entry(
			"contains asterisk no proxy",
			models.Proxy{HTTPProxy: swag.String("http://10.10.1.1:3128"), NoProxy: swag.String("*")},
			`"proxy": { "httpProxy": "http://10.10.1.1:3128", "noProxy": ["*"] }`,
			fmt.Sprintf(
				"export HTTP_PROXY=%[1]s\nexport http_proxy=%[1]s\nexport NO_PROXY=%[2]s\nexport no_proxy=%[2]s\n",
				"http://10.10.1.1:3128",
				"*",
			),
			fmt.Sprintf(
				`Environment=HTTP_PROXY=%[1]s\nEnvironment=http_proxy=%[1]s\nEnvironment=HTTPS_PROXY=\nEnvironment=https_proxy=\nEnvironment=NO_PROXY=%[2]s\nEnvironment=no_proxy=%[2]s`,
				"http://10.10.1.1:3128",
				"*",
			),
		),
	)

	It("produces a valid ignition v3.1 spec by default", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())

		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config.Ignition.Version).To(Equal("3.1.0"))
	})

	// TODO(deprecate-ignition-3.1.0)
	It("produces a valid ignition v3.1 spec with overrides", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())

		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		numOfFiles := len(config.Storage.Files)

		infraEnv.IgnitionConfigOverride = `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err = builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())

		config, report, err = config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config.Ignition.Version).To(Equal("3.1.0"))
		Expect(len(config.Storage.Files)).To(Equal(numOfFiles + 1))
	})

	It("produces a valid ignition spec with internal overrides", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())

		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config.Ignition.Version).To(Equal("3.1.0"))
		numOfFiles := len(config.Storage.Files)
		numOfUnits := len(config.Systemd.Units)
		ironicIgn := `{ "ignition": { "version": "3.2.0" }, "storage": { "files": [ { "group": { }, "overwrite": false, "path": "/etc/ironic-python-agent.conf", "user": { }, "contents": { "source": "data:text/plain,%0A%5BDEFAULT%5D%0Aapi_url%20%3D%20https%3A%2F%2Fironic.redhat.com%3A6385%0Ainspection_callback_url%20%3D%20https%3A%2F%2Fironic.redhat.com%3A5050%2Fv1%2Fcontinue%0Ainsecure%20%3D%20True%0A%0Acollect_lldp%20%3D%20True%0Aenable_vlan_interfaces%20%3D%20all%0Ainspection_collectors%20%3D%20default%2Cextra-hardware%2Clogs%0Ainspection_dhcp_all_interfaces%20%3D%20True%0A", "verification": { } }, "mode": 420 } ] }, "systemd": { "units": [ { "contents": "[Unit]\nDescription=Ironic Agent\nAfter=network-online.target\nWants=network-online.target\n[Service]\nEnvironment=\"HTTP_PROXY=\"\nEnvironment=\"HTTPS_PROXY=\"\nEnvironment=\"NO_PROXY=\"\nTimeoutStartSec=0\nExecStartPre=/bin/podman pull some-ironic-image --tls-verify=false --authfile=/etc/authfile.json\nExecStart=/bin/podman run --privileged --network host --mount type=bind,src=/etc/ironic-python-agent.conf,dst=/etc/ironic-python-agent/ignition.conf --mount type=bind,src=/dev,dst=/dev --mount type=bind,src=/sys,dst=/sys --mount type=bind,src=/run/dbus/system_bus_socket,dst=/run/dbus/system_bus_socket --mount type=bind,src=/,dst=/mnt/coreos --env \"IPA_COREOS_IP_OPTIONS=ip=dhcp\" --name ironic-agent somce-ironic-image\n[Install]\nWantedBy=multi-user.target\n", "enabled": true, "name": "ironic-agent.service" } ] } }`
		infraEnv.IgnitionConfigOverride = ironicIgn
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err = builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(text).Should(ContainSubstring("ironic-agent.service"))
		Expect(text).Should(ContainSubstring("ironic.redhat.com"))

		config2, report, err := config_32.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config2.Ignition.Version).To(Equal("3.2.0"))
		Expect(len(config2.Storage.Files)).To(Equal(numOfFiles + 1))
		Expect(len(config2.Systemd.Units)).To(Equal(numOfUnits + 1))
	})

	It("produces a valid ignition spec with v3.2 overrides", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())

		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config.Ignition.Version).To(Equal("3.1.0"))
		numOfFiles := len(config.Storage.Files)

		infraEnv.IgnitionConfigOverride = `{"ignition": {"version": "3.2.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err = builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())

		config2, report, err := config_32.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config2.Ignition.Version).To(Equal("3.2.0"))
		Expect(len(config2.Storage.Files)).To(Equal(numOfFiles + 1))
	})

	It("fails when given overrides with an incompatible version", func() {
		infraEnv.IgnitionConfigOverride = `{"ignition": {"version": "2.2.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		_, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")

		Expect(err).To(HaveOccurred())
	})

	It("applies day2 overrides successfuly", func() {
		hostID := strfmt.UUID(uuid.New().String())
		cluster.Hosts = []*models.Host{{
			ID:                      &hostID,
			RequestedHostname:       "day2worker.example.com",
			Role:                    models.HostRoleWorker,
			IgnitionConfigOverrides: `{"ignition": {"version": "3.2.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`,
		}}
		serviceBaseURL := "http://10.56.20.70:7878"

		text, err := builder.FormatSecondDayWorkerIgnitionFile(serviceBaseURL, nil, "", "", cluster.Hosts[0])

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring("/tmp/example"))
	})

	It("no multipath and iscsistart for okd - config setting", func() {
		ignitionConfig.OKDRPMsImage = "image"
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).ShouldNot(ContainSubstring("multipathd"))
		Expect(text).ShouldNot(ContainSubstring("iscsistart"))
	})

	It("okd support disabled", func() {
		ignitionConfig.EnableOKDSupport = false
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).ShouldNot(ContainSubstring("okd-overlay.servicemultipathd"))
	})

	It("no multipath and iscsistart for okd - okd payload", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		okdNewImageVersion := "4.12.0-0.okd-2022-11-20-010424"
		okdNewImageURL := "registry.ci.openshift.org/origin/release:4.12.0-0.okd-2022-11-20-010424"
		okdNewImage := &models.ReleaseImage{
			CPUArchitecture:  &common.TestDefaultConfig.CPUArchitecture,
			OpenshiftVersion: &common.TestDefaultConfig.OpenShiftVersion,
			CPUArchitectures: []string{common.TestDefaultConfig.CPUArchitecture},
			URL:              &okdNewImageURL,
			Version:          &okdNewImageVersion,
		}
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(okdNewImage, nil).Times(1)
		mockOcRelease.EXPECT().GetOKDRPMSImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("quay.io/foo/bar:okd-rpms", nil)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).ShouldNot(ContainSubstring("multipathd"))
		Expect(text).ShouldNot(ContainSubstring("iscsistart"))
	})

	It("multipath configured for non-okd", func() {
		config := ignitionConfig
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, config, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring("multipathd"))
	})

	It("iscsistart configured for non-okd", func() {
		config := ignitionConfig
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, config, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring("iscsistart"))
	})

	Context("static network config", func() {
		formattedInput := "some formated input"
		staticnetworkConfigOutput := []staticnetworkconfig.StaticNetworkConfigData{
			{
				FilePath:     "nic10.nmconnection",
				FileContents: "nic10 nmconnection content",
			},
			{
				FilePath:     "nic20.nmconnection",
				FileContents: "nic10 nmconnection content",
			},
			{
				FilePath:     "mac_interface.ini",
				FileContents: "nic10=mac10\nnic20=mac20",
			},
		}

		It("produces a valid ignition v3.1 spec with static ips paramters", func() {
			mockStaticNetworkConfig.EXPECT().GenerateStaticNetworkConfigData(gomock.Any(), formattedInput).Return(staticnetworkConfigOutput, nil).Times(1)
			infraEnv.StaticNetworkConfig = formattedInput
			infraEnv.Type = common.ImageTypePtr(models.ImageTypeFullIso)
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
			Expect(err).NotTo(HaveOccurred())
			config, report, err := config_31.Parse([]byte(text))
			Expect(err).NotTo(HaveOccurred())
			Expect(report.IsFatal()).To(BeFalse())
			count := 0
			for _, f := range config.Storage.Files {
				if strings.HasSuffix(f.Path, "nmconnection") || strings.HasSuffix(f.Path, "mac_interface.ini") {
					count += 1
				}
			}
			Expect(count).Should(Equal(3))
		})
		It("Doesn't include static network config for minimal isos", func() {
			infraEnv.StaticNetworkConfig = formattedInput
			infraEnv.Type = common.ImageTypePtr(models.ImageTypeMinimalIso)
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
			Expect(err).NotTo(HaveOccurred())
			config, report, err := config_31.Parse([]byte(text))
			Expect(err).NotTo(HaveOccurred())
			Expect(report.IsFatal()).To(BeFalse())
			count := 0
			for _, f := range config.Storage.Files {
				if strings.HasSuffix(f.Path, "nmconnection") || strings.HasSuffix(f.Path, "mac_interface.ini") {
					count += 1
				}
			}
			Expect(count).Should(Equal(0))
		})

		It("Will include static network config for minimal iso type in infraenv if overridden in call to FormatDiscoveryIgnitionFile", func() {
			mockStaticNetworkConfig.EXPECT().GenerateStaticNetworkConfigData(gomock.Any(), formattedInput).Return(staticnetworkConfigOutput, nil).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
			infraEnv.StaticNetworkConfig = formattedInput
			infraEnv.Type = common.ImageTypePtr(models.ImageTypeMinimalIso)
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, string(models.ImageTypeFullIso))
			Expect(err).NotTo(HaveOccurred())
			config, report, err := config_31.Parse([]byte(text))
			Expect(err).NotTo(HaveOccurred())
			Expect(report.IsFatal()).To(BeFalse())
			count := 0
			for _, f := range config.Storage.Files {
				if strings.HasSuffix(f.Path, "nmconnection") || strings.HasSuffix(f.Path, "mac_interface.ini") {
					count += 1
				}
			}
			Expect(count).Should(Equal(3))
		})

		It("Will not include static network config for full iso type in infraenv if overridden in call to FormatDiscoveryIgnitionFile", func() {
			infraEnv.StaticNetworkConfig = formattedInput
			infraEnv.Type = common.ImageTypePtr(models.ImageTypeFullIso)
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, string(models.ImageTypeMinimalIso))
			Expect(err).NotTo(HaveOccurred())
			config, report, err := config_31.Parse([]byte(text))
			Expect(err).NotTo(HaveOccurred())
			Expect(report.IsFatal()).To(BeFalse())
			count := 0
			for _, f := range config.Storage.Files {
				if strings.HasSuffix(f.Path, "nmconnection") || strings.HasSuffix(f.Path, "mac_interface.ini") {
					count += 1
				}
			}
			Expect(count).Should(Equal(0))
		})
	})

	Context("mirror registries config", func() {

		It("produce ignition with mirror registries config", func() {
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(true).Times(1)
			mockMirrorRegistriesConfigBuilder.EXPECT().GetMirrorCA().Return([]byte("some ca config"), nil).Times(1)
			mockMirrorRegistriesConfigBuilder.EXPECT().GetMirrorRegistries().Return([]byte("some mirror registries config"), nil).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
			Expect(err).NotTo(HaveOccurred())
			config, report, err := config_31.Parse([]byte(text))
			Expect(err).NotTo(HaveOccurred())
			Expect(report.IsFatal()).To(BeFalse())
			count := 0
			for _, f := range config.Storage.Files {
				if strings.HasSuffix(f.Path, "registries.conf") || strings.HasSuffix(f.Path, "domain.crt") {
					count += 1
				}
			}
			Expect(count).Should(Equal(2))
		})
	})

	It("Adds NTP sources script and systemd service when one additional NTP source is given", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)

		// Generate a ignition config adding one additional NTP source:
		infraEnv.AdditionalNtpSources = "ntp1.example.com"
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
		Expect(err).ToNot(HaveOccurred())

		// Parse the generated configuration:
		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())

		// Check the script:
		var scriptFile *types_31.File
		for _, file := range config.Storage.Files {
			if file.Path == "/usr/local/bin/add-ntp-sources.sh" {
				scriptFile = new(types_31.File)
				*scriptFile = file
				break
			}
		}
		Expect(scriptFile).ToNot(BeNil())
		Expect(scriptFile.Mode).ToNot(BeNil())
		Expect(*scriptFile.Mode).To(Equal(0755))
		Expect(scriptFile.User.Name).ToNot(BeNil())
		Expect(*scriptFile.User.Name).To(Equal("root"))
		Expect(scriptFile.Contents.Source).ToNot(BeNil())
		Expect(*scriptFile.Contents.Source).ToNot(BeEmpty())

		// Check that the script contains the line that will be added to the chrony configuration file:
		scriptData, err := dataurl.DecodeString(*scriptFile.Contents.Source)
		Expect(err).ToNot(HaveOccurred())
		scriptText := string(scriptData.Data)
		Expect(scriptText).To(MatchRegexp(`(?m)^server ntp1.example.com iburst$`))

		// Check the systemd service:
		var serviceUnit *types_31.Unit
		for _, unit := range config.Systemd.Units {
			if unit.Name == "add-ntp-sources.service" {
				serviceUnit = new(types_31.Unit)
				*serviceUnit = unit
				break
			}
		}
		Expect(serviceUnit).ToNot(BeNil())
		Expect(serviceUnit.Enabled).ToNot(BeNil())
		Expect(*serviceUnit.Enabled).To(BeTrue())
		Expect(serviceUnit.Contents).ToNot(BeNil())
		Expect(*serviceUnit.Contents).ToNot(BeEmpty())
	})

	It("Adds NTP sources script and systemd service when two additional NTP sources are given", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)

		// Generate a ignition config adding one additional NTP source:
		infraEnv.AdditionalNtpSources = "ntp1.example.com,ntp2.example.com"
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
		Expect(err).ToNot(HaveOccurred())

		// Parse the generated configuration:
		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())

		// Check the script:
		var scriptFile *types_31.File
		for _, file := range config.Storage.Files {
			if file.Path == "/usr/local/bin/add-ntp-sources.sh" {
				scriptFile = new(types_31.File)
				*scriptFile = file
				break
			}
		}
		Expect(scriptFile).ToNot(BeNil())
		Expect(scriptFile.Mode).ToNot(BeNil())
		Expect(*scriptFile.Mode).To(Equal(0755))
		Expect(scriptFile.User.Name).ToNot(BeNil())
		Expect(*scriptFile.User.Name).To(Equal("root"))
		Expect(scriptFile.Contents.Source).ToNot(BeNil())
		Expect(*scriptFile.Contents.Source).ToNot(BeEmpty())

		// Check that the script contains the line that will be added to the chrony configuration file:
		scriptData, err := dataurl.DecodeString(*scriptFile.Contents.Source)
		Expect(err).ToNot(HaveOccurred())
		scriptText := string(scriptData.Data)
		Expect(scriptText).To(MatchRegexp(`(?m)^server ntp1.example.com iburst$`))
		Expect(scriptText).To(MatchRegexp(`(?m)^server ntp2.example.com iburst$`))

		// Check the systemd service:
		var serviceUnit *types_31.Unit
		for _, unit := range config.Systemd.Units {
			if unit.Name == "add-ntp-sources.service" {
				serviceUnit = new(types_31.Unit)
				*serviceUnit = unit
				break
			}
		}
		Expect(serviceUnit).ToNot(BeNil())
		Expect(serviceUnit.Enabled).ToNot(BeNil())
		Expect(*serviceUnit.Enabled).To(BeTrue())
		Expect(serviceUnit.Contents).ToNot(BeNil())
		Expect(*serviceUnit.Contents).ToNot(BeEmpty())
	})

	It("Doesn't add NTP sources script and systemd service when no additional NTP sources are given", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)

		// Generate a ignition config without additional NTP sources
		infraEnv.AdditionalNtpSources = ""
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
		Expect(err).ToNot(HaveOccurred())

		// Parse the generated configuration:
		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())

		// Check that there is no script:
		var scriptFile *types_31.File
		for _, file := range config.Storage.Files {
			if file.Path == "/usr/local/bin/add-ntp-sources.sh" {
				scriptFile = new(types_31.File)
				*scriptFile = file
				break
			}
		}
		Expect(scriptFile).To(BeNil())

		// Check that there is no systemd service:
		var serviceUnit *types_31.Unit
		for _, unit := range config.Systemd.Units {
			if unit.Name == "add-ntp-sources.service" {
				serviceUnit = new(types_31.Unit)
				*serviceUnit = unit
				break
			}
		}
		Expect(serviceUnit).To(BeNil())
	})

	It("NTP sources script adds entries to existing 'chrony.conf' configuration file", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)

		// Generate a ignition config with two additional NTP sources:
		infraEnv.AdditionalNtpSources = "ntp1.example.com,ntp2.example.com"
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
		Expect(err).ToNot(HaveOccurred())

		// Parse the generated configuration:
		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())

		// Find the script:
		var scriptFile *types_31.File
		for _, file := range config.Storage.Files {
			if file.Path == "/usr/local/bin/add-ntp-sources.sh" {
				scriptFile = new(types_31.File)
				*scriptFile = file
				break
			}
		}
		scriptData, err := dataurl.DecodeString(*scriptFile.Contents.Source)
		Expect(err).ToNot(HaveOccurred())
		scriptText := string(scriptData.Data)

		// Create a temporary directory for the script and for the configuration file that
		// it will modify:
		tmpDir, err := os.MkdirTemp("", "*.test")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tmpDir)

		// Create the configuration file that will be modified by the script:
		configPath := filepath.Join(tmpDir, "chrony.conf")
		configFile, err := os.OpenFile(configPath, os.O_CREATE|os.O_WRONLY, 0600)
		Expect(err).ToNot(HaveOccurred())
		_, err = fmt.Fprintf(configFile, "makestep 1.0 3\n")
		Expect(err).ToNot(HaveOccurred())
		err = configFile.Close()
		Expect(err).ToNot(HaveOccurred())

		// Modify the script so that it will write to the configuration file that we
		// created, and write it to the temporary directory.
		scriptText = strings.ReplaceAll(scriptText, "/etc/chrony.conf", configPath)
		scriptPath := filepath.Join(tmpDir, "add-ntp-sources.sh")
		err = os.WriteFile(scriptPath, []byte(scriptText), 0700) // #nosec G306
		Expect(err).ToNot(HaveOccurred())

		// Execute the script:
		scriptCmd := exec.Command(scriptPath)
		scriptCmd.Stdout = GinkgoWriter
		scriptCmd.Stderr = GinkgoWriter
		err = scriptCmd.Run()
		Expect(err).ToNot(HaveOccurred())

		// Read the modified configuration file:
		configData, err := os.ReadFile(configPath)
		Expect(err).ToNot(HaveOccurred())
		configText := string(configData)

		// Check that the additional NTP servers have been added:
		Expect(configText).To(MatchRegexp(`(?m)^server ntp1.example.com iburst$`))
		Expect(configText).To(MatchRegexp(`(?m)^server ntp2.example.com iburst$`))

		// Check that the original config has been preserved:
		Expect(configText).To(MatchRegexp("(?m)^makestep 1.0 3$"))
	})
})

var _ = Describe("Ignition SSH key building", func() {
	var (
		ctrl                              *gomock.Controller
		infraEnv                          common.InfraEnv
		builder                           IgnitionBuilder
		mockStaticNetworkConfig           *staticnetworkconfig.MockStaticNetworkConfig
		mockMirrorRegistriesConfigBuilder *mirrorregistries.MockMirrorRegistriesConfigBuilder
		infraEnvID                        strfmt.UUID
		mockOcRelease                     *oc.MockRelease
		mockVersionHandler                *versions.MockHandler
		ignitionConfig                    IgnitionConfig
	)
	buildIgnitionAndAssertSubString := func(SSHPublicKey string, shouldExist bool, subStr string) {
		ignitionConfig = IgnitionConfig{EnableOKDSupport: true}
		infraEnv.SSHAuthorizedKey = SSHPublicKey
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, ignitionConfig, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())
		if shouldExist {
			Expect(text).Should(ContainSubstring(subStr))
		} else {
			Expect(text).ShouldNot(ContainSubstring(subStr))
		}
	}

	BeforeEach(func() {
		infraEnvID = strfmt.UUID("a64fff36-dcb1-11ea-87d0-0242ac130003")
		ctrl = gomock.NewController(GinkgoT())
		mockStaticNetworkConfig = staticnetworkconfig.NewMockStaticNetworkConfig(ctrl)
		mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
		mockOcRelease = oc.NewMockRelease(ctrl)
		mockVersionHandler = versions.NewMockHandler(ctrl)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		infraEnv = common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:            &infraEnvID,
				PullSecretSet: false,
			},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		var err error
		builder, err = NewBuilder(logrus.New(), mockStaticNetworkConfig, mockMirrorRegistriesConfigBuilder, mockOcRelease, mockVersionHandler)
		Expect(err).ToNot(HaveOccurred())
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("when empty or invalid input", func() {
		It("white_space_string should return an empty string", func() {
			buildIgnitionAndAssertSubString("  \n  \n \t \n  ", false, "sshAuthorizedKeys")
		})
		It("Empty string should return an empty string", func() {
			buildIgnitionAndAssertSubString("", false, "sshAuthorizedKeys")
		})
	})
	Context("when ssh key exists, escape when needed", func() {
		It("Single key without needed escaping", func() {
			buildIgnitionAndAssertSubString("ssh-rsa key coyote@acme.com", true, `"sshAuthorizedKeys":["ssh-rsa key coyote@acme.com"]`)
		})
		It("Multiple keys without needed escaping", func() {
			buildIgnitionAndAssertSubString("ssh-rsa key coyote@acme.com\nssh-rsa key2 coyote@acme.com",
				true,
				`"sshAuthorizedKeys":["ssh-rsa key coyote@acme.com","ssh-rsa key2 coyote@acme.com"]`)
		})
		It("Single key with escaping", func() {
			buildIgnitionAndAssertSubString(`ssh-rsa key coyote\123@acme.com`, true, `"sshAuthorizedKeys":["ssh-rsa key coyote\\123@acme.com"]`)
		})
		It("Multiple keys with escaping", func() {
			buildIgnitionAndAssertSubString(`ssh-rsa key coyote\123@acme.com
			ssh-rsa key2 coyote@acme.com`,
				true,
				`"sshAuthorizedKeys":["ssh-rsa key coyote\\123@acme.com","ssh-rsa key2 coyote@acme.com"]`)
		})
		It("Multiple keys with escaping and white space", func() {
			buildIgnitionAndAssertSubString(`
			ssh-rsa key coyote\123@acme.com

			ssh-rsa key2 c\0899oyote@acme.com
			`, true, `"sshAuthorizedKeys":["ssh-rsa key coyote\\123@acme.com","ssh-rsa key2 c\\0899oyote@acme.com"]`)
		})
	})
})

var _ = Describe("FormatSecondDayWorkerIgnitionFile", func() {

	var (
		ctrl                              *gomock.Controller
		log                               logrus.FieldLogger
		builder                           IgnitionBuilder
		mockStaticNetworkConfig           *staticnetworkconfig.MockStaticNetworkConfig
		mockMirrorRegistriesConfigBuilder *mirrorregistries.MockMirrorRegistriesConfigBuilder
		mockHost                          *models.Host
		mockOcRelease                     *oc.MockRelease
		mockVersionHandler                *versions.MockHandler
	)

	BeforeEach(func() {
		log = common.GetTestLog()
		ctrl = gomock.NewController(GinkgoT())
		mockStaticNetworkConfig = staticnetworkconfig.NewMockStaticNetworkConfig(ctrl)
		mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
		mockHost = &models.Host{Inventory: hostInventory}
		var err error
		builder, err = NewBuilder(log, mockStaticNetworkConfig, mockMirrorRegistriesConfigBuilder, mockOcRelease, mockVersionHandler)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("test custom ignition endpoint", func() {

		It("are rendered properly without ca cert and token", func() {
			ign, err := builder.FormatSecondDayWorkerIgnitionFile("http://url.com", nil, "", "", mockHost)
			Expect(err).NotTo(HaveOccurred())

			ignConfig, _, err := config_31.Parse(ign)
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(ignConfig.Ignition.Config.Merge[0].Source)).Should(Equal("http://url.com"))
			Expect(ignConfig.Ignition.Config.Merge[0].HTTPHeaders).Should(HaveLen(0))
			Expect(ignConfig.Ignition.Security.TLS.CertificateAuthorities).Should(HaveLen(0))
		})

		It("are rendered properly with token", func() {
			token := "xyzabc123"
			ign, err := builder.FormatSecondDayWorkerIgnitionFile("http://url.com", nil, token, "", mockHost)
			Expect(err).NotTo(HaveOccurred())

			ignConfig, _, err := config_31.Parse(ign)
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(ignConfig.Ignition.Config.Merge[0].Source)).Should(Equal("http://url.com"))
			Expect(ignConfig.Ignition.Config.Merge[0].HTTPHeaders).Should(HaveLen(1))
			Expect(ignConfig.Ignition.Config.Merge[0].HTTPHeaders[0].Name).Should(Equal("Authorization"))
			Expect(swag.StringValue(ignConfig.Ignition.Config.Merge[0].HTTPHeaders[0].Value)).Should(Equal("Bearer " + token))
			Expect(ignConfig.Ignition.Security.TLS.CertificateAuthorities).Should(HaveLen(0))
		})

		It("are rendered properly with ca cert", func() {
			ca := "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
				"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
				"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"
			encodedCa := base64.StdEncoding.EncodeToString([]byte(ca))
			ign, err := builder.FormatSecondDayWorkerIgnitionFile("https://url.com", &encodedCa, "", "", mockHost)
			Expect(err).NotTo(HaveOccurred())

			ignConfig, _, err := config_31.Parse(ign)
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(ignConfig.Ignition.Config.Merge[0].Source)).Should(Equal("https://url.com"))
			Expect(ignConfig.Ignition.Config.Merge[0].HTTPHeaders).Should(HaveLen(0))
			Expect(ignConfig.Ignition.Security.TLS.CertificateAuthorities).Should(HaveLen(1))
			Expect(swag.StringValue(ignConfig.Ignition.Security.TLS.CertificateAuthorities[0].Source)).Should(Equal("data:text/plain;base64," + encodedCa))
		})

		It("are rendered properly with ca cert and token", func() {
			token := "xyzabc123"
			ca := "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
				"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
				"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"
			encodedCa := base64.StdEncoding.EncodeToString([]byte(ca))
			ign, err := builder.FormatSecondDayWorkerIgnitionFile("https://url.com", &encodedCa, token, "", mockHost)

			Expect(err).NotTo(HaveOccurred())

			ignConfig, _, err := config_31.Parse(ign)
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(ignConfig.Ignition.Config.Merge[0].Source)).Should(Equal("https://url.com"))
			Expect(ignConfig.Ignition.Config.Merge[0].HTTPHeaders).Should(HaveLen(1))
			Expect(ignConfig.Ignition.Config.Merge[0].HTTPHeaders[0].Name).Should(Equal("Authorization"))
			Expect(swag.StringValue(ignConfig.Ignition.Config.Merge[0].HTTPHeaders[0].Value)).Should(Equal("Bearer " + token))
			Expect(ignConfig.Ignition.Security.TLS.CertificateAuthorities).Should(HaveLen(1))
			Expect(swag.StringValue(ignConfig.Ignition.Security.TLS.CertificateAuthorities[0].Source)).Should(Equal("data:text/plain;base64," + encodedCa))
		})
	})
})

var _ = Describe("OKD overrides", func() {
	var (
		ctrl                               *gomock.Controller
		infraEnv                           common.InfraEnv
		builder                            IgnitionBuilder
		mockStaticNetworkConfig            *staticnetworkconfig.MockStaticNetworkConfig
		mockMirrorRegistriesConfigBuilder  *mirrorregistries.MockMirrorRegistriesConfigBuilder
		infraEnvID                         strfmt.UUID
		mockOcRelease                      *oc.MockRelease
		mockVersionHandler                 *versions.MockHandler
		ocpImage, okdOldImage, okdNewImage *models.ReleaseImage
		defaultCfg, okdCfg                 IgnitionConfig
	)

	BeforeEach(func() {
		infraEnvID = strfmt.UUID("a64fff36-dcb1-11ea-87d0-0242ac130003")
		ctrl = gomock.NewController(GinkgoT())
		mockStaticNetworkConfig = staticnetworkconfig.NewMockStaticNetworkConfig(ctrl)
		mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
		mockVersionHandler = versions.NewMockHandler(ctrl)
		mockOcRelease = oc.NewMockRelease(ctrl)
		infraEnv = common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:            &infraEnvID,
				PullSecretSet: false,
			},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		var err error
		builder, err = NewBuilder(logrus.New(), mockStaticNetworkConfig, mockMirrorRegistriesConfigBuilder, mockOcRelease, mockVersionHandler)
		Expect(err).ToNot(HaveOccurred())
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		ocpImage = common.TestDefaultConfig.ReleaseImage
		okdOldImageVersion := "4.11.0-0.okd-2022-11-19-050030"
		okdOldImageURL := "quay.io/openshift/okd:4.11.0-0.okd-2022-11-19-050030"
		okdOldImage = &models.ReleaseImage{
			CPUArchitecture:  &common.TestDefaultConfig.CPUArchitecture,
			OpenshiftVersion: &common.TestDefaultConfig.OpenShiftVersion,
			CPUArchitectures: []string{common.TestDefaultConfig.CPUArchitecture},
			URL:              &okdOldImageURL,
			Version:          &okdOldImageVersion,
		}
		okdNewImageVersion := "4.12.0-0.okd-2022-11-20-010424"
		okdNewImageURL := "registry.ci.openshift.org/origin/release:4.12.0-0.okd-2022-11-20-010424"
		okdNewImage = &models.ReleaseImage{
			CPUArchitecture:  &common.TestDefaultConfig.CPUArchitecture,
			OpenshiftVersion: &common.TestDefaultConfig.OpenShiftVersion,
			CPUArchitectures: []string{common.TestDefaultConfig.CPUArchitecture},
			URL:              &okdNewImageURL,
			Version:          &okdNewImageVersion,
		}
		defaultCfg = IgnitionConfig{EnableOKDSupport: true}
		okdCfg = IgnitionConfig{
			EnableOKDSupport: true,
			OKDRPMsImage:     "quay.io/okd/foo:bar",
		}
	})

	checkOKDFiles := func(text string, err error, present bool) {
		Expect(err).NotTo(HaveOccurred())
		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		count := 0
		for _, f := range config.Storage.Files {
			if f.Path == "/usr/local/bin/okd-binaries.sh" {
				count += 1
				continue
			}
			if f.Path == "/etc/systemd/system/release-image-pivot.service.d/wait-for-okd.conf" {
				count += 1
				continue
			}
			if f.Path == "/etc/systemd/system/agent.service.d/wait-for-okd.conf" {
				count += 1
				continue
			}
		}
		if present {
			Expect(count).Should(Equal(3))
		} else {
			Expect(count).Should(Equal(0))
		}
	}

	AfterEach(func() {
		ctrl.Finish()
	})

	It("OKD_RPMS config option unset", func() {
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ocpImage, nil).Times(1)
		mockOcRelease.EXPECT().GetOKDRPMSImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("some error"))
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, defaultCfg, false, auth.TypeRHSSO, string(models.ImageTypeMinimalIso))
		checkOKDFiles(text, err, false)
	})
	It("OKD_RPMS config option not set, OKD release has no RPM image", func() {
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(okdOldImage, nil).Times(1)
		mockOcRelease.EXPECT().GetOKDRPMSImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("some error"))
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, defaultCfg, false, auth.TypeRHSSO, string(models.ImageTypeMinimalIso))
		checkOKDFiles(text, err, false)
	})
	It("OKD_RPMS config option set, OKD release has no RPM image", func() {
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, okdCfg, false, auth.TypeRHSSO, string(models.ImageTypeMinimalIso))
		checkOKDFiles(text, err, true)
	})
	It("OKD_RPMS config option not set, RPM image present in release payload", func() {
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(okdNewImage, nil).Times(1)
		mockOcRelease.EXPECT().GetOKDRPMSImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("quay.io/foo/bar:okd-rpms", nil)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, defaultCfg, false, auth.TypeRHSSO, string(models.ImageTypeMinimalIso))
		checkOKDFiles(text, err, true)
	})
})
