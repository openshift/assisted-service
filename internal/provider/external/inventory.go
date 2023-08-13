package external

import (
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
)

func (p *baseExternalProvider) CleanPlatformValuesFromDBUpdates(_ map[string]interface{}) error {
	return nil
}

func (p *baseExternalProvider) SetPlatformUsages(
	usages map[string]models.Usage,
	usageApi usage.API) error {
	props := &map[string]interface{}{
		"platform_type": p.Name()}
	usageApi.Add(usages, usage.PlatformSelectionUsage, props)
	return nil
}
