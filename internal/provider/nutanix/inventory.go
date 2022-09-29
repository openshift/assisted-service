package nutanix

import (
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
)

func (p *nutanixProvider) CleanPlatformValuesFromDBUpdates(_ map[string]interface{}) error {
	return nil
}

func (p *nutanixProvider) SetPlatformValuesInDBUpdates(_ *models.Platform, _ map[string]interface{}) error {
	return nil
}

func (p *nutanixProvider) SetPlatformUsages(
	usages map[string]models.Usage,
	usageApi usage.API) error {
	props := &map[string]interface{}{
		"platform_type": p.Name()}
	usageApi.Add(usages, usage.PlatformSelectionUsage, props)
	usageApi.Add(usages, usage.NutanixIntegration, props)
	return nil
}
