package cluster

import (
	"context"
	"testing"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"go.uber.org/mock/gomock"
)

func TestClusterManagedDNSManifests(t *testing.T) {
	for _, tc := range []struct {
		name       string
		featureSet string
		gate       string
		wantErr    bool
	}{
		{name: "CustomNoUpgrade enabled skips dnsmasq", featureSet: "CustomNoUpgrade", gate: "true"},
		{name: "DevPreview skips dnsmasq", featureSet: "DevPreviewNoUpgrade"},
		{name: "CustomNoUpgrade disabled retains dnsmasq", featureSet: "CustomNoUpgrade", gate: "false"},
		{name: "invalid gate prevents all manifest writes", featureSet: "CustomNoUpgrade", gate: "invalid", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			manifests := network.NewMockManifestsGeneratorAPI(ctrl)
			operators := operators.NewMockAPI(ctrl)
			manager := &Manager{
				log:                   logrus.New(),
				manifestsGeneratorAPI: manifests,
				rp:                    &refreshPreprocessor{operatorsAPI: operators},
			}
			cluster := &common.Cluster{Cluster: models.Cluster{
				ControlPlaneCount:      1,
				Platform:               &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
				InstallConfigOverrides: installConfigOverrides(tc.featureSet, tc.gate),
			}}
			ctx := context.Background()
			if !tc.wantErr {
				manifests.EXPECT().AddChronyManifest(ctx, gomock.Any(), cluster).Return(nil)
				if tc.gate == "false" {
					manifests.EXPECT().IsSNODNSMasqEnabled().Return(true)
					manifests.EXPECT().AddDnsmasqForSingleNode(ctx, gomock.Any(), cluster).Return(nil)
				}
				operators.EXPECT().GenerateManifests(ctx, cluster).Return(nil)
				manifests.EXPECT().AddTelemeterManifest(ctx, gomock.Any(), cluster).Return(nil)
				manifests.EXPECT().AddDiskEncryptionManifest(ctx, gomock.Any(), cluster).Return(nil)
				manifests.EXPECT().AddNicReapply(ctx, gomock.Any(), cluster).Return(nil)
			}
			if err := manager.GenerateAdditionalManifests(ctx, cluster); (err != nil) != tc.wantErr {
				t.Fatalf("got error %v, want error=%v", err, tc.wantErr)
			}
		})
	}
}

func installConfigOverrides(featureSet, gate string) string {
	overrides := `{"featureSet":"` + featureSet + `"}`
	if gate == "" {
		return overrides
	}

	return `{"featureSet":"` + featureSet + `","featureGates":["UnifiedClusterManagedDNSAndLB=` + gate + `"]}`
}
