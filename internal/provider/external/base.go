package external

import (
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type externalProvider struct {
	Log          logrus.FieldLogger
	platformType models.PlatformType
}

// NewExternalProvider creates a new none platform provider.
func NewExternalProvider(log logrus.FieldLogger, platformType models.PlatformType) provider.Provider {
	return &externalProvider{
		Log:          log,
		platformType: platformType,
	}
}

// Name returns the name of the provider
func (p *externalProvider) Name() models.PlatformType {
	return p.platformType
}

func (p *externalProvider) IsHostSupported(_ *models.Host) (bool, error) {
	return true, nil
}

func (p *externalProvider) AreHostsSupported(_ []*models.Host) (bool, error) {
	return true, nil
}
