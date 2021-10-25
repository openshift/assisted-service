package ovirt

import (
	"errors"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/models"
)

func setPlatformValues(platform *installcfg.OvirtInstallConfigPlatform, clusterPlatform *models.OvirtPlatform) {
	if clusterPlatform != nil {
		if clusterPlatform.ClusterID != nil {
			platform.ClusterID = *clusterPlatform.ClusterID
		}
		if clusterPlatform.StorageDomainID != nil {
			platform.StorageDomainID = *clusterPlatform.StorageDomainID
		}
		if clusterPlatform.NetworkName != nil {
			platform.NetworkName = *clusterPlatform.NetworkName
		}
		if clusterPlatform.VnicProfileID != nil {
			platform.VnicProfileID = *clusterPlatform.VnicProfileID
		}
	}
}

func (p ovirtProvider) AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	if cluster.Platform.Ovirt != nil {
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
		setPlatformValues(ovirtPlatform, cluster.Platform.Ovirt)
		cfg.Platform = installcfg.Platform{
			Ovirt: ovirtPlatform,
		}
		cfg.Compute[0].Replicas = 0
	}
	return nil
}
