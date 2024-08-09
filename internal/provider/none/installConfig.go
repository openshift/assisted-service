package none

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider"
)

func (p noneProvider) AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	cfg.Platform = installcfg.Platform{
		None: &installcfg.PlatformNone{},
	}

	provider.ConfigureUserManagedNetworkingInInstallConfig(p.Log, cluster, cfg)

	return nil
}
