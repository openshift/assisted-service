package provider

import (
	"github.com/openshift/assisted-service/models"
)

// Provider contains functions which are required to support installing on a specific platform.
type Provider interface {
	// Name returns the name of the platform.
	Name() models.PlatformType
}
