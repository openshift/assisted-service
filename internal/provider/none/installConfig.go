package none

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
)

func (p noneProvider) AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	cfg.Platform = installcfg.Platform{
		None: &installcfg.PlatformNone{},
	}

	bootstrapCidr := network.GetPrimaryMachineCidrForUserManagedNetwork(cluster, p.Log)
	if bootstrapCidr != "" {
		p.Log.Infof("None-Platform or SNO: Selected bootstrap machine network CIDR %s for cluster %s", bootstrapCidr, cluster.ID.String())
		machineNetwork := []installcfg.MachineNetwork{}
		cluster.MachineNetworks = network.GetMachineNetworksFromBoostrapHost(cluster, p.Log)
		for _, net := range cluster.MachineNetworks {
			machineNetwork = append(machineNetwork, installcfg.MachineNetwork{Cidr: string(net.Cidr)})
		}
		cfg.Networking.MachineNetwork = machineNetwork
		cfg.Networking.NetworkType = swag.StringValue(cluster.NetworkType)
	} else {
		cfg.Networking.MachineNetwork = nil
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
