package provider

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
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
