package external

import (
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type externalProvider struct {
	baseExternalProvider
}

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
