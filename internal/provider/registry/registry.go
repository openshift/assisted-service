package registry

import (
	"errors"
	"fmt"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/internal/provider/baremetal"
	"github.com/openshift/assisted-service/internal/provider/external"
	"github.com/openshift/assisted-service/internal/provider/none"
	"github.com/openshift/assisted-service/internal/provider/nutanix"
	"github.com/openshift/assisted-service/internal/provider/vsphere"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

// ErrNoSuchProvider is returned or thrown in panic when the specified provider is not registered.
var ErrNoSuchProvider = errors.New("no provider with the specified name registered")

// ErrProviderUnSet is returned or thrown in panic when provider has not been set.
var ErrProviderUnSet = errors.New("provider has not been set")

//go:generate mockgen --build_flags=--mod=mod -package registry -destination mock_providerregistry.go . ProviderRegistry
type ProviderRegistry interface {
	Registry
	// GetSupportedProvidersByHosts returns a slice of all the providers names which support
	// installation with the given hosts
	GetSupportedProvidersByHosts(hosts []*models.Host) ([]models.PlatformType, error)
	// AddPlatformToInstallConfig adds the provider platform to the installconfig platform field,
	// sets platform fields from values within the cluster model.
	AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error
	// SetPlatformUsages uses the usageApi to update platform specific usages
	SetPlatformUsages(p *models.Platform, usages map[string]models.Usage, usageApi usage.API) error
	// IsHostSupported checks if the provider supports the host
	IsHostSupported(p *models.Platform, host *models.Host) (bool, error)
	// AreHostsSupported checks if the provider supports the hosts
	AreHostsSupported(p *models.Platform, hosts []*models.Host) (bool, error)
	// PreCreateManifestsHook allows the provider to perform additional tasks required before the cluster manifests are created
	PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error
	// PostCreateManifestsHook allows the provider to perform additional tasks required after the cluster manifests are created
	PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error
}

// Registry registers the providers to their names.
//
//go:generate mockgen --build_flags=--mod=mod -package registry -destination mock_registry.go . Registry
type Registry interface {
	// Register registers a provider.
	Register(provider provider.Provider)
	// Get returns a provider registered to a name.
	// if provider is not registered returns an ErrNoSuchProvider
	Get(platform *models.Platform) (provider.Provider, error)
}

type registry struct {
	providers []provider.Provider
}

// NewProviderRegistry creates a new copy of a Registry.
func NewProviderRegistry() ProviderRegistry {
	return &registry{
		providers: []provider.Provider{},
	}
}

func (r *registry) Register(provider provider.Provider) {
	r.providers = append(r.providers, provider)
}

func (r *registry) Get(platform *models.Platform) (provider.Provider, error) {
	if platform == nil {
		return nil, ErrNoSuchProvider
	}

	for _, provider := range r.providers {
		if provider.IsProviderForPlatform(platform) {
			return provider, nil
		}
	}
	return nil, ErrNoSuchProvider
}

// Name returns the name of the provider
func (r *registry) Name() models.PlatformType {
	return ""
}

func (r *registry) AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	currentProvider, err := r.Get(cluster.Platform)
	if err != nil {
		return fmt.Errorf("error adding platform to install config, platform provider wasn't set: %w", err)
	}
	return currentProvider.AddPlatformToInstallConfig(cfg, cluster)
}

func (r *registry) SetPlatformUsages(
	p *models.Platform, usages map[string]models.Usage, usageApi usage.API) error {
	currentProvider, err := r.Get(p)
	if err != nil {
		return fmt.Errorf("error adding platform to install config, platform provider wasn't set: %w", err)
	}
	return currentProvider.SetPlatformUsages(usages, usageApi)
}

func (r *registry) IsHostSupported(p *models.Platform, host *models.Host) (bool, error) {
	currentProvider, err := r.Get(p)
	if err != nil {
		return false, fmt.Errorf("error while checking if hosts are supported by platform %s, error %w",
			common.PlatformTypeValue(p.Type), err)
	}
	return currentProvider.IsHostSupported(host)
}

func (r *registry) AreHostsSupported(p *models.Platform, hosts []*models.Host) (bool, error) {
	currentProvider, err := r.Get(p)
	if err != nil {
		return false, fmt.Errorf("error while checking if hosts are supported by platform %s, error %w",
			common.PlatformTypeValue(p.Type), err)
	}
	return currentProvider.AreHostsSupported(hosts)
}

func (r *registry) GetSupportedProvidersByHosts(hosts []*models.Host) ([]models.PlatformType, error) {
	var clusterSupportedPlatforms []models.PlatformType
	if len(hosts) == 0 {
		return nil, nil
	}
	for _, p := range r.providers {
		supported, err := p.AreHostsSupported(hosts)
		if err != nil {
			return nil, fmt.Errorf(
				"error while checking if hosts are supported by platform %s, error %w",
				p.Name(), err)
		}
		if supported {
			clusterSupportedPlatforms = append(clusterSupportedPlatforms, p.Name())
		}
	}
	return clusterSupportedPlatforms, nil
}

func (r *registry) PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	if cluster == nil || cluster.Platform == nil {
		return errors.New("unable to get the platform type")
	}
	currentProvider, err := r.Get(cluster.Platform)
	if err != nil {
		return fmt.Errorf("error while running pre creation manifests hook on platform %s, error %w",
			currentProvider.Name(), err)
	}
	return currentProvider.PreCreateManifestsHook(cluster, envVars, workDir)
}

func (r *registry) PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	if cluster == nil || cluster.Platform == nil {
		return errors.New("unable to get the platform type")
	}
	currentProvider, err := r.Get(cluster.Platform)
	if err != nil {
		return fmt.Errorf("error while running post creation manifests hook on platform %s, error %w",
			currentProvider.Name(), err)
	}
	return currentProvider.PostCreateManifestsHook(cluster, envVars, workDir)
}

func InitProviderRegistry(log logrus.FieldLogger) ProviderRegistry {
	providerRegistry := NewProviderRegistry()
	providerRegistry.Register(vsphere.NewVsphereProvider(log))
	providerRegistry.Register(baremetal.NewBaremetalProvider(log))
	providerRegistry.Register(none.NewNoneProvider(log))
	providerRegistry.Register(nutanix.NewNutanixProvider(log))
	providerRegistry.Register(external.NewOciExternalProvider(log))
	providerRegistry.Register(external.NewExternalProvider(log))
	return providerRegistry
}
