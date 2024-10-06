package provider

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

const (
	DbFieldPlatformType = "platform_type"
)

// Provider contains functions which are required to support installing on a specific platform.
//
//go:generate mockgen --build_flags=--mod=mod -package provider -destination mock_base_provider.go . Provider
type Provider interface {
	// Name returns the name of the platform.
	Name() models.PlatformType
	// IsProviderForCluster returns true if the provider is compatible with the platform given in input
	IsProviderForPlatform(platform *models.Platform) bool
	// AddPlatformToInstallConfig adds the provider platform to the installconfig platform field,
	// sets platform fields from values within the cluster model.
	AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error
	// CleanPlatformValuesFromDBUpdates remove platform specific values from the `updates` data structure
	CleanPlatformValuesFromDBUpdates(updates map[string]interface{}) error
	// SetPlatformUsages uses the usageApi to update platform specific usages
	SetPlatformUsages(usages map[string]models.Usage, usageApi usage.API) error
	// IsHostSupported checks if the provider supports the host
	IsHostSupported(hosts *models.Host) (bool, error)
	// AreHostsSupported checks if the provider supports the hosts
	AreHostsSupported(host []*models.Host) (bool, error)
	// PreCreateManifestsHook allows the provider to perform additional tasks required before the cluster manifests are created
	PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error
	// PostCreateManifestsHook allows the provider to perform additional tasks required after the cluster manifests are created
	PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error
}

func GetMachineNetworkForUserManagedNetworking(log logrus.FieldLogger, cluster *common.Cluster) []installcfg.MachineNetwork {
	bootstrapCidr := network.GetPrimaryMachineCidrForUserManagedNetwork(cluster, log)
	if bootstrapCidr != "" {
		log.Infof("Selected bootstrap machine network CIDR %s for cluster %s", bootstrapCidr, cluster.ID.String())
		var machineNetwork []installcfg.MachineNetwork
		cluster.MachineNetworks = network.GetMachineNetworksFromBoostrapHost(cluster, log)
		for _, net := range cluster.MachineNetworks {
			machineNetwork = append(machineNetwork, installcfg.MachineNetwork{Cidr: string(net.Cidr)})
		}
		return machineNetwork
	}

	return nil
}

func replaceMachineNetworkIfNeeded(log logrus.FieldLogger, cluster *common.Cluster, cfg *installcfg.InstallerConfigBaremetal) {
	bootstrapHost := common.GetBootstrapHost(cluster)
	if bootstrapHost == nil {
		log.Warnf("GetBootstrapHost: failed to get bootstrap host for cluster %s", lo.FromPtr(cluster.ID))
		return
	}
	nodeIpAllocations, err := network.GenerateNonePlatformAddressAllocation(cluster, log)
	if err != nil {
		log.WithError(err).Warnf("failed to get node ip allocations for cluster %s", lo.FromPtr(cluster.ID))
		return
	}
	allocation, ok := nodeIpAllocations[lo.FromPtr(bootstrapHost.ID)]
	if !ok {
		log.Warnf("node ip allocation for bootstrap host %s in cluster %s is missing", lo.FromPtr(bootstrapHost.ID), lo.FromPtr(cluster.ID))
		return
	}
	isIpv4 := network.IsIPV4CIDR(allocation.Cidr)
	for i := range cfg.Networking.MachineNetwork {
		if network.IsIPV4CIDR(cfg.Networking.MachineNetwork[i].Cidr) == isIpv4 {
			if cfg.Networking.MachineNetwork[i].Cidr != allocation.Cidr {
				log.Infof("Replacing machine network %s with %s", cfg.Networking.MachineNetwork[i].Cidr, allocation.Cidr)
				cfg.Networking.MachineNetwork[i].Cidr = allocation.Cidr
			}
			break
		}
	}
}

func ConfigureUserManagedNetworkingInInstallConfig(log logrus.FieldLogger, cluster *common.Cluster, cfg *installcfg.InstallerConfigBaremetal) {
	cfg.Networking.MachineNetwork = GetMachineNetworkForUserManagedNetworking(log, cluster)
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
		replaceMachineNetworkIfNeeded(log, cluster, cfg)
	}

}
