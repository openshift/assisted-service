package ovirt

import (
	"fmt"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

//
type ovirtProvider struct {
	Log logrus.FieldLogger
}

// NewOvirtProvider creates a new oVirt provider.
func NewOvirtProvider(log logrus.FieldLogger) provider.Provider {
	return &ovirtProvider{
		Log: log,
	}
}

// Name returns the name of the provider
func (p *ovirtProvider) Name() models.PlatformType {
	return models.PlatformTypeOvirt
}

func (p *ovirtProvider) IsHostSupported(host *models.Host) (bool, error) {
	hostInventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		return false, fmt.Errorf("error marshaling host to inventory, error %w", err)
	}
	return hostInventory.SystemVendor.Manufacturer == OvirtManufacturer, nil
}

func (p *ovirtProvider) AreHostsSupported(hosts []*models.Host) (bool, error) {
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
