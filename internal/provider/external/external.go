package external

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type externalProvider struct {
	baseExternalProvider
}

// baseExternalProvider provides a generic implementation suitable for any platforms relying on the external platform.
func NewExternalProvider(log logrus.FieldLogger) provider.Provider {
	p := &externalProvider{
		baseExternalProvider: baseExternalProvider{
			Log: log,
		},
	}
	p.Provider = p
	return p
}

func (p *externalProvider) AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	cfg.Platform = installcfg.Platform{
		External: &installcfg.ExternalInstallConfigPlatform{
			PlatformName:           *cluster.Platform.External.PlatformName,
			CloudControllerManager: installcfg.CloudControllerManager(*cluster.Platform.External.CloudControllerManager),
		},
	}

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
			cfg.BootstrapInPlace = &installcfg.BootstrapInPlace{InstallationDisk: hostutil.GetHostInstallationPath(bootstrap)}
		}
	}

	return nil
}

func (p *externalProvider) IsProviderForPlatform(platform *models.Platform) bool {
	if platform == nil ||
		platform.Type == nil {
		return false
	}

	if *platform.Type == models.PlatformTypeExternal &&
		platform.External != nil {
		return true
	}

	return false
}
