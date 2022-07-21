package ovirt

import (
	"errors"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
)

func setPlatformValues(platform *installcfg.OvirtInstallConfigPlatform) {
	platform.ClusterID = PhOvirtClusterID
	platform.StorageDomainID = PhStorageDomainID
	platform.NetworkName = PhNetworkName
	platform.VnicProfileID = PhVnicProfileID
}

func (p ovirtProvider) AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	if len(cluster.APIVip) == 0 {
		return errors.New("invalid cluster parameters, APIVip must be provided")
	}
	if len(cluster.IngressVip) == 0 {
		return errors.New("invalid cluster parameters, IngressVip must be provided")
	}
	ovirtPlatform := &installcfg.OvirtInstallConfigPlatform{
		APIVIP:     cluster.APIVip,
		IngressVIP: cluster.IngressVip,
	}
	setPlatformValues(ovirtPlatform)
	cfg.Platform = installcfg.Platform{
		Ovirt: ovirtPlatform,
	}
	return nil
}
