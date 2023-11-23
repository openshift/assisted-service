package external

import (
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type externalProvider struct {
	baseExternalProvider
}

// baseExternalProvider provides a generic implementation suitable for any platforms relying on the external platform.
func NewExternalProvider(log logrus.FieldLogger) provider.Provider {
	p := &externalProvider{
		baseExternalProvider: baseExternalProvider{
			Log: log,
		},
	}
	p.Provider = p
	return p
}

func (p *externalProvider) Name() models.PlatformType {
	return models.PlatformTypeExternal
}

func (p *externalProvider) IsProviderForPlatform(platform *models.Platform) bool {
	if platform == nil ||
		platform.Type == nil {
		return false
	}

	if *platform.Type == models.PlatformTypeExternal &&
		platform.External != nil {
		return true
	}

	return false
}
