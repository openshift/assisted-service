package vsphere

import (
	"errors"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider"
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
	vsPlatform := &installcfg.VsphereInstallConfigPlatform{}

	if !swag.BoolValue(cluster.UserManagedNetworking) {
		if cluster.APIVip == "" {
			return errors.New("invalid cluster parameters, APIVip must be provided")
		}

		if cluster.IngressVip == "" {
			return errors.New("invalid cluster parameters, IngressVip must be provided")
		}

		vsPlatform.APIVIP = cluster.APIVip
		vsPlatform.IngressVIP = cluster.IngressVip
	} else {
		cfg.Networking.MachineNetwork = provider.GetMachineNetworkForUserManagedNetworking(p.Log, cluster)
		if cluster.NetworkType != nil {
			cfg.Networking.NetworkType = swag.StringValue(cluster.NetworkType)
		}
	}

	setPlatformValues(vsPlatform)
	cfg.Platform = installcfg.Platform{
		Vsphere: vsPlatform,
	}
	return nil
}
