package baremetal

import (
	"errors"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
)

func (p *baremetalProvider) SetPlatformValuesInDBUpdates(_ *models.Platform, _ map[string]interface{}) error {
	return nil
}

func (p *baremetalProvider) CleanPlatformValuesFromDBUpdates(_ map[string]interface{}) error {
	return nil
}

func (p *baremetalProvider) GetActualSchedulableMasters(cluster *common.Cluster) (bool, error) {
	if cluster == nil {
		return false, errors.New("unexpected 'nil' cluster")
	}
	if cluster.SchedulableMasters != nil {
		return *cluster.SchedulableMasters, nil
	}
	if cluster.EnabledHostCount < minimalEnabledHostCount {
		return true, nil
	}
	return false, nil
}

func (p *baremetalProvider) SetPlatformUsages(
	_ *models.Platform,
	usages map[string]models.Usage,
	usageApi usage.API) error {
	usageApi.Remove(usages, usage.PlatformSelectionUsage)
	return nil
}
