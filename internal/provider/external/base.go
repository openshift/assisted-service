package external

import (
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

// baseExternalProvider provides a default implementation suitable for platforms relying on the external platform.
// Compose it and implement Name() to fullfil the Provider interface.
type baseExternalProvider struct {
	provider.Provider
	Log logrus.FieldLogger
}

func (p *baseExternalProvider) IsHostSupported(_ *models.Host) (bool, error) {
	return true, nil
}

func (p *baseExternalProvider) AreHostsSupported(hosts []*models.Host) (bool, error) {
	return true, nil
}
