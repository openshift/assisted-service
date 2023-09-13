package nutanix

import (
	"errors"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
)

func setPlatformValues(platform *installcfg.NutanixInstallConfigPlatform) {
	// Add placeholders to make it easier to replace in day2
	platform.PrismCentral = installcfg.NutanixPrismCentral{
		Endpoint: installcfg.NutanixEndpoint{
			Address: PhPCAddress,
			Port:    PhPCPort,
		},
		Username: PhUsername,
		Password: PhPassword,
	}
	platform.PrismElements = []installcfg.NutanixPrismElement{
		{
			Endpoint: installcfg.NutanixEndpoint{
				Address: PhPEAddress,
				Port:    PhPEPort,
			},
			UUID: PhPUUID,
			Name: PhEName,
		},
	}
	platform.SubnetUUIDs = []strfmt.UUID{
		strfmt.UUID(PhSubnetUUID),
	}
}

func (p nutanixProvider) AddPlatformToInstallConfig(
	cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	nPlatform := &installcfg.NutanixInstallConfigPlatform{}
	if !swag.BoolValue(cluster.UserManagedNetworking) {
		if len(cluster.APIVips) == 0 {
			return errors.New("invalid cluster parameters, APIVip must be provided")
		}

		if len(cluster.IngressVips) == 0 {
			return errors.New("invalid cluster parameters, IngressVip must be provided")
		}

		if featuresupport.IsFeatureAvailable(models.FeatureSupportLevelIDDUALSTACKVIPS, cluster.OpenshiftVersion, swag.String(cluster.CPUArchitecture), cluster.HighAvailabilityMode) {
			nPlatform.APIVIPs = network.GetApiVips(cluster)
			nPlatform.IngressVIPs = network.GetIngressVips(cluster)
		} else {
			nPlatform.APIVIPs = []string{network.GetApiVips(cluster)[0]}
			nPlatform.IngressVIPs = []string{network.GetIngressVips(cluster)[0]}
			nPlatform.DeprecatedAPIVIP = network.GetApiVipById(cluster, 0)
			nPlatform.DeprecatedIngressVIP = network.GetIngressVipById(cluster, 0)
		}
	} else {
		cfg.Networking.MachineNetwork = provider.GetMachineNetworkForUserManagedNetworking(p.Log, cluster)
		if cluster.NetworkType != nil {
			cfg.Networking.NetworkType = swag.StringValue(cluster.NetworkType)
		}
	}
	setPlatformValues(nPlatform)
	cfg.Platform = installcfg.Platform{
		Nutanix: nPlatform,
	}
	return nil
}
