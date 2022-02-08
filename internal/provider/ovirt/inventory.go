package ovirt

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
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

func (p *ovirtProvider) SetPlatformValuesInDBUpdates(
	platformParams *models.Platform, updates map[string]interface{}) error {
	if platformParams.Ovirt == nil {
		return p.CleanPlatformValuesFromDBUpdates(updates)
	}
	updates[DbFieldFqdn] = platformParams.Ovirt.Fqdn
	updates[DbFieldUsername] = platformParams.Ovirt.Username
	updates[DbFieldPassword] = platformParams.Ovirt.Password
	updates[DbFieldInsecure] = platformParams.Ovirt.Insecure
	if platformParams.Ovirt.CaBundle != nil {
		caBundle := []byte(*platformParams.Ovirt.CaBundle)
		if err := common.VerifyCaBundle(caBundle); err != nil {
			return errors.Wrap(err, "unable to verify the CA bundle")
		}
		updates[DbFieldCaBundle] = platformParams.Ovirt.CaBundle
	}
	updates[DbFieldClusterID] = platformParams.Ovirt.ClusterID
	updates[DbFieldStorageDomainID] = platformParams.Ovirt.StorageDomainID
	updates[DbFieldNetworkName] = platformParams.Ovirt.NetworkName
	updates[DbFieldVnicProfileID] = platformParams.Ovirt.VnicProfileID
	return nil
}

func (p *ovirtProvider) GetActualSchedulableMasters(cluster *common.Cluster) (bool, error) {
	if cluster == nil {
		return false, errors.New("unexpected 'nil' cluster")
	}
	return swag.BoolValue(cluster.SchedulableMasters), nil
}

func (p *ovirtProvider) SetPlatformUsages(
	platformParams *models.Platform,
	usages map[string]models.Usage,
	usageApi usage.API) error {
	withCredentials := platformParams.Ovirt != nil &&
		platformParams.Ovirt.Password != nil &&
		platformParams.Ovirt.Username != nil
	props := &map[string]interface{}{
		"platform_type":    p.Name(),
		"with_credentials": withCredentials}
	usageApi.Add(usages, usage.PlatformSelectionUsage, props)
	return nil
}
