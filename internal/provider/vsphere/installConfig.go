package vsphere

import (
	"errors"
	"fmt"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
)

func setPlatformValues(openshiftVersion string, platform *installcfg.VsphereInstallConfigPlatform) {
	// Add placeholders to make it easier to replace in day2
	if isLessThan, err := common.BaseVersionLessThan("4.13", openshiftVersion); isLessThan || err != nil {
		platform.DeprecatedCluster = PhCluster
		platform.DeprecatedVCenter = PhVcenter
		platform.DeprecatedNetwork = PhNetwork
		platform.DeprecatedDefaultDatastore = PhDefaultDatastore
		platform.DeprecatedUsername = PhUsername
		platform.DeprecatedPassword = PhPassword
		platform.DeprecatedDatacenter = PhDatacenter
		return
	}

	platform.VCenters = []installcfg.VsphereVCenter{
		{
			Datacenters: []string{PhDatacenter},
			Password:    PhPassword,
			Server:      PhVcenter,
			Username:    PhUsername,
		},
	}

	platform.FailureDomains = []installcfg.VsphereFailureDomain{
		{
			Name:   "assisted-generated-failure-domain",
			Region: "assisted-generated-region",
			Server: PhVcenter,
			Topology: installcfg.VsphereFailureDomainTopology{
				ComputeCluster: fmt.Sprintf("/%s/host/%s", PhDatacenter, PhCluster),
				Datacenter:     PhDatacenter,
				Datastore:      fmt.Sprintf("/%s/datastore/%s", PhDatacenter, PhDefaultDatastore),
				Folder:         fmt.Sprintf("/%s/vm/%s", PhDatacenter, PhFolder),
				Networks:       []string{PhNetwork},
			},
			Zone: "assisted-generated-zone",
		},
	}
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

		if featuresupport.IsFeatureSupported(cluster.OpenshiftVersion, models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS) {
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

	setPlatformValues(cluster.OpenshiftVersion, vsPlatform)
	cfg.Platform = installcfg.Platform{
		Vsphere: vsPlatform,
	}
	return nil
}
