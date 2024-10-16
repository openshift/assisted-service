package builder

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

const minimalOpenShiftVersionForImageDigestSupport = "4.14.0-0.0"

//go:generate mockgen -source=builder.go -package=builder -destination=mock_installcfg.go
type InstallConfigBuilder interface {
	GetInstallConfig(cluster *common.Cluster, clusterInfraenvs []*common.InfraEnv, rhRootCA string, mrConfiguration *v1beta1.MirrorRegistryConfiguration) ([]byte, error)
	ValidateInstallConfigPatch(cluster *common.Cluster, clusterInfraenvs []*common.InfraEnv, patch string, mrConfiguration *v1beta1.MirrorRegistryConfiguration) error
}

type installConfigBuilder struct {
	log                     logrus.FieldLogger
	mirrorRegistriesBuilder mirrorregistries.ServiceMirrorRegistriesConfigBuilder
	providerRegistry        registry.ProviderRegistry
}

func NewInstallConfigBuilder(
	log logrus.FieldLogger,
	mirrorRegistriesBuilder mirrorregistries.ServiceMirrorRegistriesConfigBuilder,
	providerRegistry registry.ProviderRegistry) InstallConfigBuilder {
	return &installConfigBuilder{
		log:                     log,
		mirrorRegistriesBuilder: mirrorRegistriesBuilder,
		providerRegistry:        providerRegistry,
	}
}

func (i *installConfigBuilder) countHostsByRole(cluster *common.Cluster, role models.HostRole) int {
	var count int
	for _, host := range cluster.Hosts {
		if common.GetEffectiveRole(host) == role {
			count += 1
		}
	}
	return count
}

func (i *installConfigBuilder) generateNoProxy(cluster *common.Cluster) string {
	noProxy := strings.TrimSpace(cluster.NoProxy)
	if noProxy == "*" {
		return noProxy
	}

	splitNoProxy := funk.FilterString(strings.Split(noProxy, ","), func(s string) bool { return s != "" })

	// Add internal OCP DNS domain
	splitNoProxy = append(splitNoProxy, "."+cluster.Name+"."+cluster.BaseDNSDomain)

	// Add cluster networks, service networks and machine networks
	for _, clusterNetwork := range cluster.ClusterNetworks {
		splitNoProxy = append(splitNoProxy, string(clusterNetwork.Cidr))
	}
	for _, serviceNetwork := range cluster.ServiceNetworks {
		splitNoProxy = append(splitNoProxy, string(serviceNetwork.Cidr))
	}
	for _, machineNetwork := range cluster.MachineNetworks {
		splitNoProxy = append(splitNoProxy, string(machineNetwork.Cidr))
	}

	return strings.Join(splitNoProxy, ",")
}

func (i *installConfigBuilder) getBasicInstallConfig(cluster *common.Cluster, configuration *v1beta1.MirrorRegistryConfiguration) (*installcfg.InstallerConfigBaremetal, error) {
	networkType := swag.StringValue(cluster.NetworkType)
	i.log.Infof("Selected network type %s for cluster %s", networkType, cluster.ID.String())
	cfg := &installcfg.InstallerConfigBaremetal{
		APIVersion: "v1",
		BaseDomain: cluster.BaseDNSDomain,
		Metadata: struct {
			Name string `json:"name"`
		}{
			Name: cluster.Name,
		},
		Compute: []struct {
			Hyperthreading string `json:"hyperthreading,omitempty"`
			Name           string `json:"name"`
			Replicas       int    `json:"replicas"`
		}{
			{
				Hyperthreading: i.getHypethreadingConfiguration(cluster, "worker"),
				Name:           string(models.HostRoleWorker),
				Replicas:       i.countHostsByRole(cluster, models.HostRoleWorker),
			},
		},
		ControlPlane: struct {
			Hyperthreading string `json:"hyperthreading,omitempty"`
			Name           string `json:"name"`
			Replicas       int    `json:"replicas"`
		}{
			Hyperthreading: i.getHypethreadingConfiguration(cluster, "master"),
			Name:           string(models.HostRoleMaster),
			Replicas:       i.countHostsByRole(cluster, models.HostRoleMaster),
		},
		PullSecret: cluster.PullSecret,
		SSHKey:     cluster.SSHPublicKey,
	}

	cfg.Networking.NetworkType = networkType

	for _, network := range cluster.ClusterNetworks {
		cfg.Networking.ClusterNetwork = append(cfg.Networking.ClusterNetwork,
			installcfg.ClusterNetwork{Cidr: string(network.Cidr), HostPrefix: int(network.HostPrefix)})
	}
	for _, network := range cluster.MachineNetworks {
		cfg.Networking.MachineNetwork = append(cfg.Networking.MachineNetwork,
			installcfg.MachineNetwork{Cidr: string(network.Cidr)})
	}
	for _, network := range cluster.ServiceNetworks {
		cfg.Networking.ServiceNetwork = append(cfg.Networking.ServiceNetwork, string(network.Cidr))
	}

	if cluster.HTTPProxy != "" || cluster.HTTPSProxy != "" {
		cfg.Proxy = &installcfg.Proxy{
			HTTPProxy:  cluster.HTTPProxy,
			HTTPSProxy: cluster.HTTPSProxy,
			NoProxy:    i.generateNoProxy(cluster),
		}
	}

	if err := i.handleMirrorRegistry(cfg, cluster, configuration); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (i *installConfigBuilder) handleMirrorRegistry(cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster, configuration *v1beta1.MirrorRegistryConfiguration) error {
	isOpenShiftVersionRecentEnough, err := common.BaseVersionGreaterOrEqual(minimalOpenShiftVersionForImageDigestSupport, cluster.OpenshiftVersion)
	if err != nil {
		return err
	}

	i.log.Infof("found OpenShift version %s, setting mirror registry %+v", cluster.OpenshiftVersion, configuration)
	if isOpenShiftVersionRecentEnough {
		if err = i.setImageDigestMirrorSet(cfg, configuration); err != nil {
			return err
		}
	} else {
		// If version does not support ImageDigestSources, set ImageContent
		if err = i.setImageContentSources(cfg, configuration); err != nil {
			return err
		}
	}

	return nil
}

func (i *installConfigBuilder) setImageDigestMirrorSet(cfg *installcfg.InstallerConfigBaremetal, configuration *v1beta1.MirrorRegistryConfiguration) error {
	var imageDigestSourceList []installcfg.ImageDigestSource

	if configuration != nil {
		i.log.Infof("Found cluster mirror configuration, setting imageDigestSourceList with %+v", configuration.MirrorRegistryConfigurationInfo.ImageDigestMirrors)
		imageDigestSourceList = make([]installcfg.ImageDigestSource, len(configuration.MirrorRegistryConfigurationInfo.ImageDigestMirrors))
		for k, _registry := range configuration.MirrorRegistryConfigurationInfo.ImageDigestMirrors {
			mirrors := make([]string, len(_registry.Mirrors))
			for j, mirror := range _registry.Mirrors {
				mirrors[j] = string(mirror)
			}
			imageDigestSourceList[k] = installcfg.ImageDigestSource{Source: _registry.Source, Mirrors: mirrors}
		}
	} else if i.mirrorRegistriesBuilder.IsMirrorRegistriesConfigured() {
		i.log.Infof("Found service mirror configuration, setting imageDigestSourceList")
		mirrorRegistriesConfigs, err := i.mirrorRegistriesBuilder.ExtractLocationMirrorDataFromRegistries()
		if err != nil {
			i.log.WithError(err).Errorf("Failed to get the mirror registries conf need for ImageDigestSources")
			return err
		}
		imageDigestSourceList = make([]installcfg.ImageDigestSource, len(mirrorRegistriesConfigs))
		for j, mirrorRegistriesConfig := range mirrorRegistriesConfigs {
			imageDigestSourceList[j] = installcfg.ImageDigestSource{Source: mirrorRegistriesConfig.Location, Mirrors: mirrorRegistriesConfig.Mirror}
		}
	}

	cfg.ImageDigestSources = imageDigestSourceList
	return nil
}

func (i *installConfigBuilder) setImageContentSources(cfg *installcfg.InstallerConfigBaremetal, configuration *v1beta1.MirrorRegistryConfiguration) error {
	var imageContentSourceList []installcfg.ImageContentSource

	if configuration != nil {
		i.log.Infof("Found cluster mirror configuration, setting imageContentSourceList with %+v", configuration.MirrorRegistryConfigurationInfo.ImageDigestMirrors)
		imageContentSourceList = make([]installcfg.ImageContentSource, len(configuration.MirrorRegistryConfigurationInfo.ImageDigestMirrors))
		for j, _registry := range configuration.MirrorRegistryConfigurationInfo.ImageDigestMirrors {
			mirrors := make([]string, len(_registry.Mirrors))
			for k, mirror := range _registry.Mirrors {
				mirrors[k] = string(mirror)
			}
			imageContentSourceList[j] = installcfg.ImageContentSource{Source: _registry.Source, Mirrors: mirrors}
		}
	} else if i.mirrorRegistriesBuilder.IsMirrorRegistriesConfigured() {
		mirrorRegistriesConfigs, err := i.mirrorRegistriesBuilder.ExtractLocationMirrorDataFromRegistries()
		if err != nil {
			i.log.WithError(err).Errorf("Failed to get the mirror registries conf need for ImageContentSources")
			return err
		}
		imageContentSourceList = make([]installcfg.ImageContentSource, len(mirrorRegistriesConfigs))
		for j, mirrorRegistriesConfig := range mirrorRegistriesConfigs {
			imageContentSourceList[j] = installcfg.ImageContentSource{Source: mirrorRegistriesConfig.Location, Mirrors: mirrorRegistriesConfig.Mirror}
		}
	}

	cfg.DeprecatedImageContentSources = imageContentSourceList
	return nil
}

func (i *installConfigBuilder) applyConfigOverrides(overrides string, cfg *installcfg.InstallerConfigBaremetal) error {
	if overrides == "" {
		return nil
	}

	overrideDecoder := json.NewDecoder(strings.NewReader(overrides))
	overrideDecoder.DisallowUnknownFields()

	if err := overrideDecoder.Decode(cfg); err != nil {
		return err
	}

	return nil
}

func (i *installConfigBuilder) getInstallConfig(cluster *common.Cluster, clusterInfraenvs []*common.InfraEnv, rhRootCA string, mrConfiguration *v1beta1.MirrorRegistryConfiguration) (*installcfg.InstallerConfigBaremetal, error) {
	cfg, err := i.getBasicInstallConfig(cluster, mrConfiguration)
	if err != nil {
		return nil, err
	}

	err = i.providerRegistry.AddPlatformToInstallConfig(cfg, cluster)
	if err != nil {
		return nil, fmt.Errorf(
			"error while adding Platfom %s to install config, error is: %w", common.PlatformTypeValue(cluster.Platform.Type), err)
	}
	err = i.applyConfigOverrides(cluster.InstallConfigOverrides, cfg)
	if err != nil {
		return nil, err
	}
	caContent := i.mergeAllCASources(cluster, clusterInfraenvs, rhRootCA, cfg.AdditionalTrustBundle, mrConfiguration)
	if caContent != "" {
		cfg.AdditionalTrustBundle = caContent
	}

	return cfg, nil
}

func (i *installConfigBuilder) GetInstallConfig(cluster *common.Cluster, clusterInfraenvs []*common.InfraEnv, rhRootCA string, mrConfiguration *v1beta1.MirrorRegistryConfiguration) ([]byte, error) {
	cfg, err := i.getInstallConfig(cluster, clusterInfraenvs, rhRootCA, mrConfiguration)
	if err != nil {
		return nil, err
	}

	return json.Marshal(*cfg)
}

func (i *installConfigBuilder) ValidateInstallConfigPatch(cluster *common.Cluster, clusterInfraenvs []*common.InfraEnv, patch string, mrConfiguration *v1beta1.MirrorRegistryConfiguration) error {
	config, err := i.getInstallConfig(cluster, clusterInfraenvs, "", mrConfiguration)
	if err != nil {
		return err
	}

	err = i.applyConfigOverrides(patch, config)
	if err != nil {
		return err
	}

	return config.Validate()
}

func (i *installConfigBuilder) getHypethreadingConfiguration(cluster *common.Cluster, machineType string) string {
	switch cluster.Hyperthreading {
	case models.ClusterHyperthreadingAll:
		return "Enabled"
	case models.ClusterHyperthreadingMasters:
		if machineType == "master" {
			return "Enabled"
		}
	case models.ClusterHyperthreadingWorkers:
		if machineType == "worker" {
			return "Enabled"
		}
	}
	return "Disabled"
}

// mergeAllCASources merges all the CA sources into a single string, seperated
// by newlines. CA sources include:
// - The Red Hat root CA (used during the product's CI tests),
// - User configured mirror registry CAs
// - Additional trust bundle from the cluster's infraenvs
// - Certs from user-provided install config overrides
func (i *installConfigBuilder) mergeAllCASources(cluster *common.Cluster, clusterInfraenvs []*common.InfraEnv, rhRootCA string, installConfigOverrideCerts string, mirrorRegistryConfiguration *v1beta1.MirrorRegistryConfiguration) string {
	certs := []string{}

	if installConfigOverrideCerts != "" {
		certs = append(certs, installConfigOverrideCerts)
	}

	if rhRootCA != "" {
		certs = append(certs, rhRootCA)
	}

	if i.mirrorRegistriesBuilder.IsMirrorRegistriesConfigured() {
		caContents, err := i.mirrorRegistriesBuilder.GetMirrorCA()
		if err == nil {
			certs = append(certs, string(caContents))
		}
	}

	if mirrorRegistryConfiguration != nil {
		if mirrorRegistryConfiguration.CaBundleCrt != "" {
			certs = append(certs, mirrorRegistryConfiguration.CaBundleCrt)
		}
	}

	// Add CA trust bundles from host infraenvs
	for _, infraenv := range clusterInfraenvs {
		if infraenv.AdditionalTrustBundle != "" {
			certs = append(certs, infraenv.AdditionalTrustBundle)
		}
	}

	return strings.TrimSpace(strings.Join(certs, "\n"))
}
