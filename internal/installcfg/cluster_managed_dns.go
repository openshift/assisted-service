package installcfg

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

// UsesClusterManagedDNS checks the effective install-config FeatureGate enablement.
func UsesClusterManagedDNS(cluster *common.Cluster) (bool, error) {
	if common.IsDay2Cluster(cluster) {
		return false, nil
	}

	cfg := &InstallerConfigBaremetal{}
	cfg.ControlPlane.Replicas = int(cluster.ControlPlaneCount)
	cfg.Platform = platformFromCluster(cluster.Platform)

	if cluster.InstallConfigOverrides != "" {
		decoder := json.NewDecoder(strings.NewReader(cluster.InstallConfigOverrides))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(cfg); err != nil {
			return false, fmt.Errorf("render install config: %w", err)
		}
	}

	if cfg.Platform.None == nil || cfg.ControlPlane.Replicas != 1 {
		return false, nil
	}
	if cfg.FeatureSet == configv1.DevPreviewNoUpgrade {
		return true, nil
	}
	if cfg.FeatureSet != configv1.CustomNoUpgrade {
		return false, nil
	}

	var enabled, found bool
	for _, gate := range cfg.FeatureGates {
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

func platformFromCluster(platform *models.Platform) Platform {
	if platform == nil || platform.Type == nil {
		return Platform{}
	}

	switch *platform.Type {
	case models.PlatformTypeBaremetal:
		return Platform{Baremetal: &BareMetalInstallConfigPlatform{}}
	case models.PlatformTypeNone:
		return Platform{None: &PlatformNone{}}
	case models.PlatformTypeExternal:
		return Platform{External: &ExternalInstallConfigPlatform{}}
	case models.PlatformTypeVsphere:
		return Platform{Vsphere: &VsphereInstallConfigPlatform{}}
	case models.PlatformTypeNutanix:
		return Platform{Nutanix: &NutanixInstallConfigPlatform{}}
	default:
		return Platform{}
	}
}
