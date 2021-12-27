package vsphere

import (
	"errors"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
)

func setPlatformValues(platform *installcfg.VsphereInstallConfigPlatform) {
	// Add placeholders to make it easier to replace in day2
	platform.Cluster = PhCluster
	platform.VCenter = PhVcenter
	platform.Network = PhNetwork
	platform.DefaultDatastore = PhDefaultDatastore
	platform.Username = PhUsername
	platform.Password = PhPassword
	platform.Datacenter = PhDatacenter
}

func (p vsphereProvider) AddPlatformToInstallConfig(
	cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	if len(cluster.APIVip) == 0 {
		return errors.New("invalid cluster parameters, APIVip must be provided")
	}
	if len(cluster.IngressVip) == 0 {
		return errors.New("invalid cluster parameters, IngressVip must be provided")
	}
	vsPlatform := &installcfg.VsphereInstallConfigPlatform{
		APIVIP:     cluster.APIVip,
		IngressVIP: cluster.IngressVip,
	}
	setPlatformValues(vsPlatform)
	cfg.Platform = installcfg.Platform{
		Vsphere: vsPlatform,
	}
	return nil
}
