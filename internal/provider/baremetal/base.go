package baremetal

import (
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type baremetalProvider struct {
	Log logrus.FieldLogger
}

// NewBaremetalProvider creates a new baremetal provider.
func NewBaremetalProvider(log logrus.FieldLogger) provider.Provider {
	return &baremetalProvider{
		Log: log,
	}
}

// Name returns the name of the provider
func (p *baremetalProvider) Name() models.PlatformType {
	return models.PlatformTypeBaremetal
}

func (p *baremetalProvider) IsHostSupported(_ *models.Host) (bool, error) {
	return true, nil
}

func (p *baremetalProvider) AreHostsSupported(_ []*models.Host) (bool, error) {
	return true, nil
}

func (p *baremetalProvider) IsProviderForPlatform(platform *models.Platform) bool {
	return platform != nil &&
		platform.Type != nil &&
		*platform.Type == p.Name()
}
