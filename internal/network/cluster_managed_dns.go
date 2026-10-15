package network

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

const unifiedClusterManagedDNSAndLBFeatureGate = "UnifiedClusterManagedDNSAndLB"

// UsesClusterManagedDNS checks install-config FeatureGate enablement.
func UsesClusterManagedDNS(cluster *common.Cluster) (bool, error) {
	if common.IsDay2Cluster(cluster) || !common.IsSingleNodeCluster(cluster) ||
		cluster.Platform == nil || cluster.Platform.Type == nil || *cluster.Platform.Type != models.PlatformTypeNone ||
		cluster.InstallConfigOverrides == "" {
		return false, nil
	}

	// Cannot reuse installcfg.InstallerConfigBaremetal because of cyclic import
	var overrides struct {
		FeatureSet   configv1.FeatureSet `json:"featureSet"`
		FeatureGates []string            `json:"featureGates"`
		Platform     struct {
			None      *struct{} `json:"none"`
			Baremetal *struct{} `json:"baremetal"`
		} `json:"platform"`
		ControlPlane struct {
			Replicas *int `json:"replicas"`
		} `json:"controlPlane"`
	}
	overrides.Platform.None = &struct{}{}
	if err := json.Unmarshal([]byte(cluster.InstallConfigOverrides), &overrides); err != nil {
		return false, fmt.Errorf("parse install-config overrides: %w", err)
	}
	if overrides.Platform.None == nil || overrides.Platform.Baremetal != nil ||
		(overrides.ControlPlane.Replicas != nil && *overrides.ControlPlane.Replicas != 1) {
		return false, nil
	}
	if overrides.FeatureSet == configv1.DevPreviewNoUpgrade {
		return true, nil
	}
	if overrides.FeatureSet != configv1.CustomNoUpgrade {
		return false, nil
	}

	var enabled, found bool
	for _, gate := range overrides.FeatureGates {
		name, value, _ := strings.Cut(gate, "=")
		if name != unifiedClusterManagedDNSAndLBFeatureGate {
			continue
		}
		if found {
			return false, fmt.Errorf("feature gate %s is configured more than once", name)
		}
		found = true

		var err error
		enabled, err = strconv.ParseBool(value)
		if err != nil {
			return false, fmt.Errorf("invalid feature gate %s: %w", name, err)
		}
	}
	return enabled, nil
}
