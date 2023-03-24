package external

import (
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type externalProvider struct {
	Log logrus.FieldLogger
}

// NewExternalProvider creates a new none platform provider.
func NewExternalProvider(log logrus.FieldLogger) provider.Provider {
	return &externalProvider{
		Log: log,
	}
}

// Name returns the name of the provider
func (p *externalProvider) Name() models.PlatformType {
	return models.PlatformTypeExternal
}

func (p *externalProvider) IsHostSupported(_ *models.Host) (bool, error) {
	return true, nil
}

func (p *externalProvider) AreHostsSupported(_ []*models.Host) (bool, error) {
	return true, nil
}
