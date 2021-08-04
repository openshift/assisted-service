package vsphere

import (
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
)

func (p *vsphereProvider) CleanPlatformValuesFromDBUpdates(updates map[string]interface{}) error {
	updates[DbFieldVsphere] = nil
	updates[DbFieldUsername] = nil
	updates[DbFieldPassword] = nil
	updates[DbFieldDatacenter] = nil
	updates[DbFieldDefaultDatastore] = nil
	updates[DbFieldCluster] = nil
	updates[DbFieldNetwork] = nil
	updates[DbFieldVCenter] = nil
	updates[DbFieldFolder] = nil
	return nil
}

func (p *vsphereProvider) SetPlatformValuesInDBUpdates(
	platformParams *models.Platform, updates map[string]interface{}) error {
	if platformParams.Vsphere != nil {
		updates[DbFieldUsername] = platformParams.Vsphere.Username
		updates[DbFieldPassword] = platformParams.Vsphere.Password
		updates[DbFieldDatacenter] = platformParams.Vsphere.Datacenter
		updates[DbFieldDefaultDatastore] = platformParams.Vsphere.DefaultDatastore
		updates[DbFieldCluster] = platformParams.Vsphere.Cluster
		updates[DbFieldNetwork] = platformParams.Vsphere.Network
		updates[DbFieldVCenter] = platformParams.Vsphere.VCenter
		updates[DbFieldFolder] = platformParams.Vsphere.Folder
	}
	return nil
}

func (p *vsphereProvider) SetPlatformUsages(
	platformParams *models.Platform,
	usages map[string]models.Usage,
	usageApi usage.API) error {
	withCredentials := platformParams.Vsphere != nil &&
		platformParams.Vsphere.VCenter != nil &&
		platformParams.Vsphere.Password != nil &&
		platformParams.Vsphere.Username != nil
	props := &map[string]interface{}{
		"platform_type":    p.Name(),
		"with_credentials": withCredentials}
	usageApi.Add(usages, usage.PlatformSelectionUsage, props)
	return nil
}
