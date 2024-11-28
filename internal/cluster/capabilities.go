package cluster

import (
	"encoding/json"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/pkg/errors"
	"github.com/samber/lo"
)

func GetClusterCapabilities(cluster *common.Cluster) (*installcfg.Capabilities, error) {
	if cluster.InstallConfigOverrides == "" {
		return nil, nil
	}
	var installConfig installcfg.InstallerConfigBaremetal
	err := json.Unmarshal([]byte(cluster.InstallConfigOverrides), &installConfig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal install config overrides when checking capabilities for cluster %s", cluster.Name)
	}
	return installConfig.Capabilities, nil
}

func HasBaseCapabilities(cluster *common.Cluster, baselineCapability configv1.ClusterVersionCapabilitySet) bool {
	capabilities, err := GetClusterCapabilities(cluster)
	if err != nil || capabilities == nil {
		return false
	}
	return capabilities.BaselineCapabilitySet == baselineCapability
}

func HasAdditionalCapabilities(cluster *common.Cluster, includes []configv1.ClusterVersionCapability) bool {
	capabilities, err := GetClusterCapabilities(cluster)
	if err != nil || capabilities == nil {
		return false
	}
	return lo.Every(capabilities.AdditionalEnabledCapabilities, includes)
}
