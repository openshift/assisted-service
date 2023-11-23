package external

import (
	"fmt"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

const (
	OCIManufacturer string = "OracleCloud.com"
	OCIPlaformName  string = "oci"
)

type ociExternalProvider struct {
	baseExternalProvider
}

func NewOciExternalProvider(log logrus.FieldLogger) provider.Provider {
	p := &ociExternalProvider{
		baseExternalProvider: baseExternalProvider{
			Log: log,
		},
	}
	p.Provider = p
	return p
}

func (p *ociExternalProvider) Name() models.PlatformType {
	return models.PlatformTypeOci
}

func (p *ociExternalProvider) IsHostSupported(host *models.Host) (bool, error) {
	return IsOciHost(host)
}

func (p *ociExternalProvider) AreHostsSupported(hosts []*models.Host) (bool, error) {
	for _, h := range hosts {
		supported, err := p.IsHostSupported(h)
		if err != nil {
			return false, fmt.Errorf("error while checking if host is supported, error is: %w", err)
		}
		if !supported {
			return false, nil
		}
	}
	return true, nil
}

func (p *ociExternalProvider) AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	cfg.Platform = installcfg.Platform{
		External: &installcfg.ExternalInstallConfigPlatform{
			PlatformName:           OCIPlaformName,
			CloudControllerManager: installcfg.CloudControllerManagerTypeExternal,
		},
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
	}

	return nil
}

func (p *ociExternalProvider) IsProviderForPlatform(platform *models.Platform) bool {
	if platform == nil ||
		platform.Type == nil {
		return false
	}

	if *platform.Type == p.Name() {
		return true
	}

	if *platform.Type == models.PlatformTypeExternal &&
		platform.External != nil &&
		*platform.External.PlatformName == OCIPlaformName {
		return true
	}

	return false
}

func IsOciHost(host *models.Host) (bool, error) {
	// during the discovery there is a short time that host didn't return its inventory to the service
	if host.Inventory == "" {
		return false, nil
	}
	hostInventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		return false, fmt.Errorf("error marshaling host to inventory, error %w", err)
	}
	return hostInventory.SystemVendor.Manufacturer == OCIManufacturer, nil
}
