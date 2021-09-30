package vsphere

import (
	"errors"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/models"
)

func setPlatformValues(platform *installcfg.VsphereInstallConfigPlatform, clusterPlatform *models.VspherePlatform) {
	if clusterPlatform != nil && clusterPlatform.VCenter != nil {
		platform.VCenter = *clusterPlatform.VCenter
		platform.Username = *clusterPlatform.Username
		platform.Password = *clusterPlatform.Password
		platform.Datacenter = *clusterPlatform.Datacenter
		platform.DefaultDatastore = *clusterPlatform.DefaultDatastore
		platform.Network = *clusterPlatform.Network
		platform.Cluster = *clusterPlatform.Cluster
		if clusterPlatform.Folder != nil {
			platform.Folder = *clusterPlatform.Folder
		}
	} else {
		// This is to support adding parameters in day2
		platform.Cluster = PhCluster
		platform.VCenter = PhVcenter
		platform.Network = PhNetwork
		platform.DefaultDatastore = PhDefaultDatastore
		platform.Username = PhUsername
		platform.Password = PhPassword
		platform.Datacenter = PhDatacenter
	}
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
	setPlatformValues(vsPlatform, cluster.Platform.Vsphere)
	cfg.Platform = installcfg.Platform{
		Vsphere: vsPlatform,
	}
	return nil
}
