package baremetal

import (
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

//
type baremetalProvider struct {
	Log  logrus.FieldLogger
	name models.PlatformType
}

// NewBaremetalProvider creates a new baremetal provider.
func NewBaremetalProvider(log logrus.FieldLogger, name models.PlatformType) provider.Provider {
	return &baremetalProvider{
		Log:  log,
		name: name,
	}
}

// Name returns the name of the provider
func (p *baremetalProvider) Name() models.PlatformType {
	return p.name
}

func (p *baremetalProvider) IsHostSupported(_ *models.Host) (bool, error) {
	return true, nil
}

func (p *baremetalProvider) AreHostsSupported(_ []*models.Host) (bool, error) {
	return true, nil
}
