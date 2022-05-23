package baremetal

import (
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
)

func (p *baremetalProvider) SetPlatformValuesInDBUpdates(_ *models.Platform, _ map[string]interface{}) error {
	return nil
}

func (p *baremetalProvider) CleanPlatformValuesFromDBUpdates(_ map[string]interface{}) error {
	return nil
}

func (p *baremetalProvider) SetPlatformUsages(
	_ *models.Platform,
	usages map[string]models.Usage,
	usageApi usage.API) error {
	usageApi.Remove(usages, usage.PlatformSelectionUsage)
	return nil
}
