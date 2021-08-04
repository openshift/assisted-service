package provider

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/models"
)

// Provider contains functions which are required to support installing on a specific platform.
type Provider interface {
	// Name returns the name of the platform.
	Name() models.PlatformType
	// AddPlatformToInstallConfig adds the provider platform to the installconfig platform field,
	// sets platform fields from values within the cluster model.
	AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error
}