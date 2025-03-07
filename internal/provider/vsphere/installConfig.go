package vsphere

import (
	"errors"
	"fmt"

	"github.com/go-openapi/swag"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	errorWrap "github.com/pkg/errors"
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

func (p vsphereProvider) addLoadBalancer(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	if cluster.LoadBalancer == nil {
		return nil
	}
	switch cluster.LoadBalancer.Type {
	case models.LoadBalancerTypeClusterManaged:
		// Nothing, this is the default.
	case models.LoadBalancerTypeUserManaged:
		cfg.Platform.Vsphere.LoadBalancer = &configv1.VSpherePlatformLoadBalancer{
			Type: configv1.LoadBalancerTypeUserManaged,
		}
	default:
		return fmt.Errorf(
			"load balancer type is set to unsupported value '%s', supported values are "+
				"'%s' and '%s'",
			cluster.LoadBalancer.Type,
			models.LoadBalancerTypeClusterManaged,
			models.LoadBalancerTypeUserManaged,
		)
	}
	return nil
}

func (p vsphereProvider) AddPlatformToInstallConfig(
	cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster, infraEnvs []*common.InfraEnv) error {
	vsPlatform := &installcfg.VsphereInstallConfigPlatform{}

	if !swag.BoolValue(cluster.UserManagedNetworking) {
		if len(cluster.APIVips) == 0 {
			return errors.New("invalid cluster parameters, APIVip must be provided")
		}

		if len(cluster.IngressVips) == 0 {
			return errors.New("invalid cluster parameters, IngressVip must be provided")
		}

		if featuresupport.IsFeatureAvailable(models.FeatureSupportLevelIDDUALSTACKVIPS, cluster.OpenshiftVersion, swag.String(cluster.CPUArchitecture)) {
			vsPlatform.APIVIPs = network.GetApiVips(cluster)
			vsPlatform.IngressVIPs = network.GetIngressVips(cluster)
		} else {
			vsPlatform.APIVIPs = []string{network.GetApiVips(cluster)[0]}
			vsPlatform.IngressVIPs = []string{network.GetIngressVips(cluster)[0]}
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

	if err := p.addLoadBalancer(cfg, cluster); err != nil {
		return errorWrap.Wrap(err, "failed to set vSphere's cluster install-config.yaml load balancer as user-managed")
	}

	return nil
}
