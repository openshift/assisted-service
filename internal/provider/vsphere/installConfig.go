package vsphere

import (
	"errors"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
)

func setPlatformValues(platform *installcfg.VsphereInstallConfigPlatform) {
	// Add placeholders to make it easier to replace in day2
	platform.Cluster = PhCluster
	platform.VCenter = PhVcenter
	platform.Network = PhNetwork
	platform.DefaultDatastore = PhDefaultDatastore
	platform.Username = PhUsername
	platform.Password = PhPassword
	platform.Datacenter = PhDatacenter
}

func (p vsphereProvider) AddPlatformToInstallConfig(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	vsPlatform := &installcfg.VsphereInstallConfigPlatform{}

	if !swag.BoolValue(cluster.UserManagedNetworking) {
		if len(cluster.APIVips) == 0 {
			return errors.New("invalid cluster parameters, APIVip must be provided")
		}

		if len(cluster.IngressVips) == 0 {
			return errors.New("invalid cluster parameters, IngressVip must be provided")
		}

		if featuresupport.IsFeatureSupported(models.FeatureSupportLevelIDDUALSTACKVIPS, cluster.OpenshiftVersion, swag.String(cluster.CPUArchitecture)) {
			vsPlatform.APIVIPs = network.GetApiVips(cluster)
			vsPlatform.IngressVIPs = network.GetIngressVips(cluster)
		} else {
			vsPlatform.DeprecatedAPIVIP = network.GetApiVipById(cluster, 0)
			vsPlatform.DeprecatedIngressVIP = network.GetIngressVipById(cluster, 0)
		}
	} else {
		cfg.Networking.MachineNetwork = provider.GetMachineNetworkForUserManagedNetworking(p.Log, cluster)
		if cluster.NetworkType != nil {
			cfg.Networking.NetworkType = swag.StringValue(cluster.NetworkType)
		}
	}

	setPlatformValues(vsPlatform)
	cfg.Platform = installcfg.Platform{
		Vsphere: vsPlatform,
	}
	return nil
}
