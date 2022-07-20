package ovirt

import (
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
)

func (p *ovirtProvider) CleanPlatformValuesFromDBUpdates(updates map[string]interface{}) error {
	updates[provider.DbFieldPlatformType] = models.PlatformTypeBaremetal
	updates[DbFieldFqdn] = nil
	updates[DbFieldUsername] = nil
	updates[DbFieldPassword] = nil
	updates[DbFieldInsecure] = nil
	updates[DbFieldCaBundle] = nil
	updates[DbFieldClusterID] = nil
	updates[DbFieldStorageDomainID] = nil
	updates[DbFieldNetworkName] = nil
	updates[DbFieldVnicProfileID] = nil
	return nil
}

func (p *ovirtProvider) SetPlatformUsages(
	usages map[string]models.Usage,
	usageApi usage.API) error {
	props := &map[string]interface{}{
		"platform_type":    p.Name(),
		"with_credentials": false}
	usageApi.Add(usages, usage.PlatformSelectionUsage, props)
	return nil
}
