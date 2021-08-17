package provider

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
)

const (
	DbFieldPlatformType = "platform_type"
)

// Provider contains functions which are required to support installing on a specific platform.
type Provider interface {
	// Name returns the name of the platform.
	Name() models.PlatformType
	// AddPlatformToInstallConfig adds the provider platform to the installconfig platform field,
	// sets platform fields from values within the cluster model.
	AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error
	// SetPlatformValuesInDBUpdates updates the `updates` data structure with platform specific values
	SetPlatformValuesInDBUpdates(platformParams *models.Platform, updates map[string]interface{}) error
	// CleanPlatformValuesFromDBUpdates remove platform specific values from the `updates` data structure
	CleanPlatformValuesFromDBUpdates(updates map[string]interface{}) error
	// SetPlatformUsages uses the usageApi to update platform specific usages
	SetPlatformUsages(platformParams *models.Platform, usages map[string]models.Usage, usageApi usage.API) error
	// IsHostSupported checks if the provider supports the host
	IsHostSupported(hosts *models.Host) (bool, error)
	// AreHostsSupported checks if the provider supports the hosts
	AreHostsSupported(host []*models.Host) (bool, error)
}
