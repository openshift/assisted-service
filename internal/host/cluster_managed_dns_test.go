package host

import (
	"os"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("cluster-managed DNS validation", func() {
	var (
		previousDNSMasq string
		dnsmasqWasSet   bool
	)

	BeforeEach(func() {
		previousDNSMasq, dnsmasqWasSet = os.LookupEnv("ENABLE_SINGLE_NODE_DNSMASQ")
	})

	AfterEach(func() {
		if dnsmasqWasSet {
			Expect(os.Setenv("ENABLE_SINGLE_NODE_DNSMASQ", previousDNSMasq)).To(Succeed())
		} else {
			Expect(os.Unsetenv("ENABLE_SINGLE_NODE_DNSMASQ")).To(Succeed())
		}
	})

	DescribeTable("selects DNS validation from the rendered install config",
		func(overrides, dnsmasq string, day2, wantCheck bool, wantStatus ValidationStatus) {
			Expect(os.Setenv("ENABLE_SINGLE_NODE_DNSMASQ", dnsmasq)).To(Succeed())
			cluster := &common.Cluster{Cluster: models.Cluster{
				ControlPlaneCount:      1,
				Platform:               &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
				UserManagedNetworking:  swag.Bool(true),
				BaseDNSDomain:          "example.com",
				InstallConfigOverrides: overrides,
			}}
			if day2 {
				cluster.Kind = swag.String(models.ClusterKindAddHostsCluster)
			}

			v := &validator{}
			for _, domain := range []string{"api", "api-int", "apps"} {
				check, status, _ := v.shouldValidateDNSResolution(cluster, domain, domain)
				Expect(check).To(Equal(wantCheck), domain)
				Expect(status).To(Equal(wantStatus), domain)
			}
		},
		Entry("uses CoreDNS instead of dnsmasq",
			`{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"]}`,
			"true", false, false, ValidationSuccess),
		Entry("uses DevPreview CoreDNS instead of dnsmasq",
			`{"featureSet":"DevPreviewNoUpgrade"}`,
			"true", false, false, ValidationSuccess),
		Entry("uses CoreDNS when dnsmasq is disabled",
			`{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"]}`,
			"false", false, false, ValidationSuccess),
		Entry("uses legacy dnsmasq", "", "true", false, false, ValidationSuccess),
		Entry("uses external DNS", "", "false", false, true, ValidationStatus("")),
		Entry("requires external DNS for day two",
			`{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"]}`,
			"true", true, true, ValidationStatus("")),
		Entry("reports invalid overrides", `{"featureSet":`, "true", false, false, ValidationError),
		Entry("reports an invalid gate",
			`{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=invalid"]}`,
			"true", false, false, ValidationError),
	)
})
