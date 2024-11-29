package external

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

// baseExternalProvider provides a default implementation suitable for platforms relying on the external platform.
// Compose it and implement Name() to fullfil the Provider interface.
type baseExternalProvider struct {
	provider.Provider
	Log logrus.FieldLogger
}

func (p *baseExternalProvider) Name() models.PlatformType {
	return models.PlatformTypeExternal
}

func (p *baseExternalProvider) IsHostSupported(_ *models.Host) (bool, error) {
	return true, nil
}

func (p *baseExternalProvider) AreHostsSupported(hosts []*models.Host) (bool, error) {
	return true, nil
}

func (p *baseExternalProvider) AddPlatformToInstallConfig(
	cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster, infraEnvs []*common.InfraEnv) error {
	cfg.Platform = installcfg.Platform{
		External: &installcfg.ExternalInstallConfigPlatform{
			PlatformName:           *cluster.Platform.External.PlatformName,
			CloudControllerManager: installcfg.CloudControllerManager(*cluster.Platform.External.CloudControllerManager),
		},
	}

	provider.ConfigureUserManagedNetworkingInInstallConfig(p.Log, cluster, cfg)

	return nil
}
