package external

import (
	"github.com/go-openapi/swag"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
)

func (p externalProvider) AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	// workaround until openshift installer supports external platform
	cfg.Platform = installcfg.Platform{
		None: &installcfg.PlatformNone{},
	}

	// Enable feature set "TechPreviewNoUpgrade" in order to access External platform feature
	cfg.FeatureSet = configv1.TechPreviewNoUpgrade

	cfg.Networking.MachineNetwork = provider.GetMachineNetworkForUserManagedNetworking(p.Log, cluster)
	if cluster.NetworkType != nil {
		cfg.Networking.NetworkType = swag.StringValue(cluster.NetworkType)
	}

	if common.IsSingleNodeCluster(cluster) {

		if cfg.Networking.NetworkType == "" {
			cfg.Networking.NetworkType = models.ClusterNetworkTypeOVNKubernetes
		}

		bootstrap := common.GetBootstrapHost(cluster)
		if bootstrap != nil {
			cfg.BootstrapInPlace = installcfg.BootstrapInPlace{InstallationDisk: hostutil.GetHostInstallationPath(bootstrap)}
		}
	}

	return nil
}
