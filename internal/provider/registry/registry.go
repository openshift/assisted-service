package registry

import (
	"errors"
	"fmt"
	"github.com/openshift/assisted-service/internal/usage"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/internal/provider/baremetal"
	"github.com/openshift/assisted-service/internal/provider/vsphere"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

// ErrNoSuchProvider is returned or thrown in panic when the specified provider is not registered.
var ErrNoSuchProvider = errors.New("no provider with the specified name registered")

// ErrProviderUnSet is returned or thrown in panic when provider has not been set.
var ErrProviderUnSet = errors.New("provider has not been set")

type ProviderRegistry interface {
	Registry
	// GetSupportedProvidersByHosts returns a slice of all the providers names which support
	// installation with the given hosts
	GetSupportedProvidersByHosts(hosts []*models.Host) ([]models.PlatformType, error)
	// AddPlatformToInstallConfig adds the provider platform to the installconfig platform field,
	// sets platform fields from values within the cluster model.
	AddPlatformToInstallConfig(p models.PlatformType, cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error
	// SetPlatformValuesInDBUpdates updates the `updates` data structure with platform specific values
	SetPlatformValuesInDBUpdates(p models.PlatformType, platformParams *models.Platform, updates map[string]interface{}) error
	// SetPlatformUsages uses the usageApi to update platform specific usages
	SetPlatformUsages(p models.PlatformType, platformParams *models.Platform, usages map[string]models.Usage, usageApi usage.API) error
}

// Registry registers the providers to their names.
type Registry interface {
	// Register registers a provider.
	Register(provider provider.Provider)
	// Get returns a provider registered to a name.
	// if provider is not registered returns an ErrNoSuchProvider
	Get(name string) (provider.Provider, error)
}

type registry struct {
	providers map[string]provider.Provider
}

// NewProviderRegistry creates a new copy of a Registry.
func NewProviderRegistry() ProviderRegistry {
	return &registry{
		providers: map[string]provider.Provider{},
	}
}

func (r *registry) Register(provider provider.Provider) {
	r.providers[string(provider.Name())] = provider
}

func (r *registry) Get(name string) (provider.Provider, error) {
	if p, ok := r.providers[name]; ok {
		return p, nil
	}
	return nil, ErrNoSuchProvider
}

// Name returns the name of the provider
func (r *registry) Name() models.PlatformType {
	return ""
}

func (r *registry) AddPlatformToInstallConfig(p models.PlatformType, cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	currentProvider, err := r.Get(string(p))
	if err != nil {
		return fmt.Errorf("error adding platform to install config, platform provider wasn't set: %w", err)
	}
	return currentProvider.AddPlatformToInstallConfig(cfg, cluster)
}

func (r *registry) SetPlatformValuesInDBUpdates(
	p models.PlatformType, platformParams *models.Platform, updates map[string]interface{}) error {
	currentProvider, err := r.Get(string(p))
	if err != nil {
		return fmt.Errorf("error adding platform to install config, platform provider wasn't set: %w", err)
	}
	for _, provider := range r.providers {
		if err = provider.CleanPlatformValuesFromDBUpdates(updates); err != nil {
			return fmt.Errorf("error while removing platform %v values from database: %w", provider.Name(), err)
		}
	}
	updates[provider.DbFieldPlatformType] = platformParams.Type
	return currentProvider.SetPlatformValuesInDBUpdates(platformParams, updates)
}

func (r *registry) SetPlatformUsages(
	p models.PlatformType, platformParams *models.Platform, usages map[string]models.Usage, usageApi usage.API) error {
	currentProvider, err := r.Get(string(p))
	if err != nil {
		return fmt.Errorf("error adding platform to install config, platform provider wasn't set: %w", err)
	}
	return currentProvider.SetPlatformUsages(platformParams, usages, usageApi)
}

func InitProviderRegistry(log logrus.FieldLogger) ProviderRegistry {
	providerRegistry := NewProviderRegistry()
	providerRegistry.Register(vsphere.NewVsphereProvider(log))
	providerRegistry.Register(baremetal.NewBaremetalProvider(log))
	return providerRegistry
}
