package registry

import (
	"errors"

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

func InitProviderRegistry(log logrus.FieldLogger) ProviderRegistry {
	providerRegistry := NewProviderRegistry()
	providerRegistry.Register(vsphere.NewVsphereProvider(log))
	providerRegistry.Register(baremetal.NewBaremetalProvider(log))
	return providerRegistry
}
