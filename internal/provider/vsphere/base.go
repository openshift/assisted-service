package vsphere

import (
	"fmt"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type vsphereProvider struct {
	Log logrus.FieldLogger
}

// NewVsphereProvider creates a new vSphere provider.
func NewVsphereProvider(log logrus.FieldLogger) provider.Provider {
	return &vsphereProvider{
		Log: log,
	}
}

// Name returns the name of the provider
func (p *vsphereProvider) Name() models.PlatformType {
	return models.PlatformTypeVsphere
}

func (p *vsphereProvider) IsHostSupported(host *models.Host) (bool, error) {
	// during the discovery there is a short time that host didn't return its inventory to the service
	if host.Inventory == "" {
		return false, nil
	}
	hostInventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		return false, fmt.Errorf("error marshaling host to inventory, error %w", err)
	}
	return hostInventory.SystemVendor.Manufacturer == VmwareManufacturer, nil
}

func (p *vsphereProvider) AreHostsSupported(hosts []*models.Host) (bool, error) {
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
