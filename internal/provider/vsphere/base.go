package vsphere

import (
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

//
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
