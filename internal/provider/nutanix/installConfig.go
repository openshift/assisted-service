package nutanix

import (
	"errors"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider"
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
		if len(cluster.APIVip) == 0 {
			return errors.New("invalid cluster parameters, APIVip must be provided")
		}

		if len(cluster.IngressVip) == 0 {
			return errors.New("invalid cluster parameters, IngressVip must be provided")
		}

		nPlatform.DeprecatedAPIVIP = cluster.APIVip
		nPlatform.DeprecatedIngressVIP = cluster.IngressVip
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
