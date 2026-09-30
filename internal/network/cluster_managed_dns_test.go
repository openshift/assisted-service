package network

import (
	"testing"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

func TestUsesClusterManagedDNS(t *testing.T) {
	const enabled = `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"]}`
	const devPreview = `{"featureSet":"DevPreviewNoUpgrade"}`
	for _, tc := range []struct {
		name      string
		overrides string
		platform  models.PlatformType
		masters   int64
		day2      bool
		want      bool
		wantErr   bool
	}{
		{name: "default"},
		{name: "explicit enablement", overrides: enabled, want: true},
		{name: "explicit disablement", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=false"]}`},
		{name: "unrelated gate", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["OtherFeature=true"]}`},
		{name: "other overrides are preserved", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["OtherFeature=false","UnifiedClusterManagedDNSAndLB=true"],"fips":true}`, want: true},
		{name: "feature set required", overrides: `{"featureGates":["UnifiedClusterManagedDNSAndLB=true"]}`},
		{name: "DevPreview enables the gate", overrides: devPreview, want: true},
		{name: "TechPreview does not enable the gate", overrides: `{"featureSet":"TechPreviewNoUpgrade"}`},
		{name: "non-None platform", platform: models.PlatformTypeExternal, overrides: enabled},
		{name: "multiple masters", masters: 3, overrides: enabled},
		{name: "day two", day2: true, overrides: enabled},
		{name: "replica override", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"],"controlPlane":{"replicas":3}}`},
		{name: "null replica override preserves default", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"],"controlPlane":{"replicas":null}}`, want: true},
		{name: "platform override", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"],"platform":{"none":null,"external":{}}}`},
		{name: "baremetal takes precedence", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"],"platform":{"baremetal":{}}}`},
		{name: "None precedes External", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"],"platform":{"external":{}}}`, want: true},
		{name: "invalid JSON", overrides: `{"featureSet":`, wantErr: true},
		{name: "invalid gate list", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":true}`, wantErr: true},
		{name: "invalid gate value", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=invalid"]}`, wantErr: true},
		{name: "missing gate value", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB"]}`, wantErr: true},
		{name: "conflicting gate values", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true","UnifiedClusterManagedDNSAndLB=false"]}`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.platform == "" {
				tc.platform = models.PlatformTypeNone
			}
			if tc.masters == 0 {
				tc.masters = 1
			}
			cluster := &common.Cluster{Cluster: models.Cluster{
				ControlPlaneCount:      tc.masters,
				Platform:               &models.Platform{Type: &tc.platform},
				InstallConfigOverrides: tc.overrides,
			}}
			if tc.day2 {
				cluster.Kind = swag.String(models.ClusterKindAddHostsCluster)
			}
			got, err := UsesClusterManagedDNS(cluster)
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("got (%v, %v), want (%v, error=%v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}
