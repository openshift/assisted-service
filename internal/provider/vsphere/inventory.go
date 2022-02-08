package vsphere

import (
	"errors"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
)

func (p *vsphereProvider) CleanPlatformValuesFromDBUpdates(_ map[string]interface{}) error {
	return nil
}

func (p *vsphereProvider) SetPlatformValuesInDBUpdates(_ *models.Platform, _ map[string]interface{}) error {
	return nil
}

func (p *vsphereProvider) GetActualSchedulableMasters(cluster *common.Cluster) (bool, error) {
	if cluster == nil {
		return false, errors.New("unexpected 'nil' cluster")
	}
	return swag.BoolValue(cluster.SchedulableMasters), nil
}

func (p *vsphereProvider) SetPlatformUsages(
	platformParams *models.Platform,
	usages map[string]models.Usage,
	usageApi usage.API) error {
	props := &map[string]interface{}{
		"platform_type": p.Name()}
	usageApi.Add(usages, usage.PlatformSelectionUsage, props)
	return nil
}
