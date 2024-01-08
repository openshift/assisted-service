package none

import (
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type noneProvider struct {
	Log logrus.FieldLogger
}

// NewNoneProvider creates a new none platform provider.
func NewNoneProvider(log logrus.FieldLogger) provider.Provider {
	return &noneProvider{
		Log: log,
	}
}

// Name returns the name of the provider
func (p *noneProvider) Name() models.PlatformType {
	return models.PlatformTypeNone
}

func (p *noneProvider) IsHostSupported(_ *models.Host) (bool, error) {
	return true, nil
}

func (p *noneProvider) AreHostsSupported(_ []*models.Host) (bool, error) {
	return true, nil
}

func (p *noneProvider) IsProviderForPlatform(platform *models.Platform) bool {
	return platform != nil &&
		platform.Type != nil &&
		*platform.Type == p.Name()
}
