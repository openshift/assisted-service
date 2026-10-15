package host

import (
	"testing"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

func TestClusterManagedDNSValidation(t *testing.T) {
	const enabled = `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=true"]}`
	const devPreview = `{"featureSet":"DevPreviewNoUpgrade"}`
	for _, tc := range []struct {
		name       string
		overrides  string
		dnsmasq    string
		day2       bool
		wantCheck  bool
		wantStatus ValidationStatus
	}{
		{name: "CoreDNS replaces dnsmasq", overrides: enabled, dnsmasq: "true", wantStatus: ValidationSuccess},
		{name: "DevPreview CoreDNS replaces dnsmasq", overrides: devPreview, dnsmasq: "true", wantStatus: ValidationSuccess},
		{name: "CoreDNS with dnsmasq disabled", overrides: enabled, dnsmasq: "false", wantStatus: ValidationSuccess},
		{name: "legacy dnsmasq", dnsmasq: "true", wantStatus: ValidationSuccess},
		{name: "external DNS", dnsmasq: "false", wantCheck: true},
		{name: "day two still requires DNS", overrides: enabled, dnsmasq: "true", day2: true, wantCheck: true},
		{name: "invalid overrides", overrides: `{"featureSet":`, dnsmasq: "true", wantStatus: ValidationError},
		{name: "invalid gate", overrides: `{"featureSet":"CustomNoUpgrade","featureGates":["UnifiedClusterManagedDNSAndLB=invalid"]}`, dnsmasq: "true", wantStatus: ValidationError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENABLE_SINGLE_NODE_DNSMASQ", tc.dnsmasq)
			cluster := &common.Cluster{Cluster: models.Cluster{
				ControlPlaneCount:      1,
				Platform:               &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
				UserManagedNetworking:  swag.Bool(true),
				BaseDNSDomain:          "example.com",
				InstallConfigOverrides: tc.overrides,
			}}
			if tc.day2 {
				cluster.Kind = swag.String(models.ClusterKindAddHostsCluster)
			}
			v := &validator{}
			for _, domain := range []string{"api", "api-int", "apps"} {
				check, status, _ := v.shouldValidateDNSResolution(cluster, domain, domain)
				if check != tc.wantCheck || status != tc.wantStatus {
					t.Fatalf("%s: got (%v, %q), want (%v, %q)", domain, check, status, tc.wantCheck, tc.wantStatus)
				}
			}
		})
	}
}
