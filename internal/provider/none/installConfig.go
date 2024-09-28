package none

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/samber/lo"
)

func (p noneProvider) replaceMachineNetworkIfNeeded(cluster *common.Cluster, cfg *installcfg.InstallerConfigBaremetal) {
	bootstrapHost := common.GetBootstrapHost(cluster)
	if bootstrapHost == nil {
		p.Log.Warnf("GetBootstrapHost: failed to get bootstrap host for cluster %s", lo.FromPtr(cluster.ID))
		return
	}
	nodeIpAllocations, err := network.GenerateNonePlatformAddressAllocation(cluster, p.Log)
	if err != nil {
		p.Log.WithError(err).Warnf("failed to get node ip allocations for cluster %s", lo.FromPtr(cluster.ID))
		return
	}
	allocation, ok := nodeIpAllocations[lo.FromPtr(bootstrapHost.ID)]
	if !ok {
		p.Log.Warnf("node ip allocation for bootstrap host %s in cluster %s is missing", lo.FromPtr(bootstrapHost.ID), lo.FromPtr(cluster.ID))
		return
	}
	isIpv4 := network.IsIPV4CIDR(allocation.Cidr)
	for i := range cfg.Networking.MachineNetwork {
		if network.IsIPV4CIDR(cfg.Networking.MachineNetwork[i].Cidr) == isIpv4 {
			if cfg.Networking.MachineNetwork[i].Cidr != allocation.Cidr {
				p.Log.Infof("Replacing machine network %s with %s", cfg.Networking.MachineNetwork[i].Cidr, allocation.Cidr)
				cfg.Networking.MachineNetwork[i].Cidr = allocation.Cidr
			}
			break
		}
	}
}

func (p noneProvider) AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	cfg.Platform = installcfg.Platform{
		None: &installcfg.PlatformNone{},
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
	} else {
		p.replaceMachineNetworkIfNeeded(cluster, cfg)
	}

	return nil
}
