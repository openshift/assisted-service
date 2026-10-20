package installcfg

import (
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

type clusterManagedDNSTestCase struct {
	overrides string
	platform  models.PlatformType
	masters   int
	day2      bool
	want      bool
	wantErr   bool
}

var _ = Describe("UsesClusterManagedDNS", func() {
	DescribeTable("renders the effective install config",
		func(tc clusterManagedDNSTestCase) {
			platform := tc.platform
			if platform == "" {
				platform = models.PlatformTypeNone
			}
			masters := tc.masters
			if masters == 0 {
				masters = 1
			}

			cluster := &common.Cluster{Cluster: models.Cluster{
				ControlPlaneCount:      int64(masters),
				Platform:               &models.Platform{Type: &platform},
				InstallConfigOverrides: tc.overrides,
			}}
			if tc.day2 {
				cluster.Kind = swag.String(models.ClusterKindAddHostsCluster)
			}

			got, err := UsesClusterManagedDNS(cluster)
			if tc.wantErr {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).NotTo(HaveOccurred())
			}
			Expect(got).To(Equal(tc.want))
		},
		Entry("with the default FeatureSet", clusterManagedDNSTestCase{}),
		Entry("with explicit enablement", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"]}`,
			want:      true,
		}),
		Entry("with explicit disablement", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=false"]}`,
		}),
		Entry("with an unrelated gate", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["OtherFeature=true"]}`,
		}),
		Entry("with other install-config fields", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["OtherFeature=false","UnifiedClusterManagedDNSAndLB=true"],"fips":true}`,
			want:      true,
		}),
		Entry("without CustomNoUpgrade", clusterManagedDNSTestCase{
			overrides: `{"featureGates":["UnifiedClusterManagedDNSAndLB=true"]}`,
		}),
		Entry("with DevPreviewNoUpgrade", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"DevPreviewNoUpgrade"}`,
			want:      true,
		}),
		Entry("with TechPreviewNoUpgrade", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"TechPreviewNoUpgrade"}`,
		}),
		Entry("on a non-None platform", clusterManagedDNSTestCase{
			platform:  models.PlatformTypeExternal,
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"]}`,
		}),
		Entry("with multiple rendered control-plane replicas", clusterManagedDNSTestCase{
			masters:   3,
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"]}`,
		}),
		Entry("for a day-2 cluster", clusterManagedDNSTestCase{
			day2:      true,
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"]}`,
		}),
		Entry("with a replica override", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"],"controlPlane":{"replicas":3}}`,
		}),
		Entry("with a null replica override", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"],"controlPlane":{"replicas":null}}`,
			want:      true,
		}),
		Entry("with an effective External platform override", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"],"platform":{"none":null,"external":{}}}`,
		}),
		Entry("when None precedes an added External platform", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"],"platform":{"external":{}}}`,
			want:      true,
		}),
		Entry("with invalid JSON", clusterManagedDNSTestCase{
			overrides: `{"featureSet":`,
			wantErr:   true,
		}),
		Entry("with an invalid gate list", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":true}`,
			wantErr:   true,
		}),
		Entry("with an invalid gate value", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=invalid"]}`,
			wantErr:   true,
		}),
		Entry("with a missing gate value", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB"]}`,
			wantErr:   true,
		}),
		Entry("with conflicting gate values", clusterManagedDNSTestCase{
			overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true","UnifiedClusterManagedDNSAndLB=false"]}`,
			wantErr:   true,
		}),
	)
})
