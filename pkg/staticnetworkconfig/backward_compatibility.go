package staticnetworkconfig

import (
	"github.com/openshift/assisted-service/internal/common"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type Config struct {
	// MinVersionForNmstateService is a flag that enables the static networking flow using the nmstate service for specific OCP versions.
	MinVersionForNmstateService string `envconfig:"MIN_VERSION_FOR_NMSTATE_SERVICE" default:"4.18"`
}

func (s *StaticNetworkConfigGenerator) NMStatectlServiceSupported(version string) (bool, error) {
	// When a cluster is imported, the OpenshiftVersion isn't stored in the database.
	// Consequently, a bound InfraEnv with static networking uses the Cluster's OpenshiftVersion, which is empty.
	if version == "" {
		log.Info("ocp version is empty")
		return false, nil
	}
	versionOK, err := common.VersionGreaterOrEqual(version, s.config.MinVersionForNmstateService)
	if err != nil {
		return false, err
	}
	return versionOK, nil
}

// CheckConfigForAutoDnsSetToFalse detect whether any of the host-provided YAML configurations contain an interface with auto-dns: false.
// TODO: This is a temporary workaround and should be removed once the auto-dns:false bug is fixed
func (s *StaticNetworkConfigGenerator) CheckConfigForAutoDnsSetToFalse(staticNetworkConfigStr string) (bool, error) {
	staticNetworkConfig, err := s.decodeStaticNetworkConfig(staticNetworkConfigStr)
	if err != nil {
		s.log.WithError(err).Errorf("Failed to decode static network config")
		return false, err
	}

	for _, hostConfig := range staticNetworkConfig {
		isIncludeAutoDnsSetToFalse, err := s.hasAutoDnsSetToFalse(hostConfig.NetworkYaml)
		if err != nil {
			return false, err
		}
		if isIncludeAutoDnsSetToFalse {
			return true, nil
		}
	}
	return false, nil
}

func (s *StaticNetworkConfigGenerator) hasAutoDnsSetToFalse(networksYaml string) (bool, error) {
	var config map[string]interface{}

	// Unmarshal the YAML string into the config struct
	err := yaml.Unmarshal([]byte(networksYaml), &config)
	if err != nil {
		s.log.WithError(err).Errorf("Error unmarshalling yaml")
		return false, err
	}

	interfaces, exists := config["interfaces"]
	if !exists || interfaces == nil {
		return false, nil
	}
	interfacesSlice, ok := interfaces.([]interface{})
	if !ok {
		return false, nil
	}

	for _, iface := range interfacesSlice {
		nic := iface.(map[interface{}]interface{})
		isAutoDNSDisabled := func(ipConfig map[interface{}]interface{}) bool {
			if autoDns, exists := ipConfig["auto-dns"]; exists && autoDns == false {
				return true
			}
			return false
		}

		if ipv4, exists := nic["ipv4"].(map[interface{}]interface{}); exists && isAutoDNSDisabled(ipv4) {
			return true, nil
		}
		if ipv6, exists := nic["ipv6"].(map[interface{}]interface{}); exists && isAutoDNSDisabled(ipv6) {
			return true, nil
		}
	}
	return false, nil
}

// CheckConfigForMACIdentifier TODO: This is a temporary workaround and should be removed once the mac-identifier bug in nmstate is fixed - RHEL-72440.
func (s *StaticNetworkConfigGenerator) CheckConfigForMACIdentifier(staticNetworkConfigStr string) (bool, error) {
	staticNetworkConfig, err := s.decodeStaticNetworkConfig(staticNetworkConfigStr)
	if err != nil {
		s.log.WithError(err).Errorf("Failed to decode static network config")
		return false, err
	}

	for _, hostConfig := range staticNetworkConfig {
		isIncludeMacIdentifier, err := s.hasMACIdentifier(hostConfig.NetworkYaml)
		if err != nil {
			return false, err
		}
		if isIncludeMacIdentifier {
			return true, nil
		}
	}
	return false, nil
}

func (s *StaticNetworkConfigGenerator) hasMACIdentifier(networksYaml string) (bool, error) {
	var config map[string]interface{}

	// Unmarshal the YAML string into the config struct
	err := yaml.Unmarshal([]byte(networksYaml), &config)
	if err != nil {
		s.log.WithError(err).Errorf("Error unmarshalling yaml")
		return false, err
	}

	interfaces, exists := config["interfaces"]
	if !exists || interfaces == nil {
		return false, nil
	}
	interfacesSlice, ok := interfaces.([]interface{})
	if !ok {
		return false, nil
	}

	for _, iface := range interfacesSlice {
		nic := iface.(map[interface{}]interface{})
		identifier, exists := nic["identifier"]
		if exists && identifier == "mac-address" {
			return true, nil
		}
	}
	return false, nil
}

// ShouldUseNmstatectlService - Both static networking flows should be maintained: one without nmstate.service and one with it, since nmstate.service isn't available in all RHCOS versions.
func (s *StaticNetworkConfigGenerator) ShouldUseNmstateService(staticNetworkConfigStr, openshiftVersion string) (bool, error) {
	includesMACIdentifier, err := s.CheckConfigForMACIdentifier(staticNetworkConfigStr)
	if err != nil {
		return false, err
	}

	includeAutoDnsSetToFalse, err := s.CheckConfigForAutoDnsSetToFalse(staticNetworkConfigStr)
	if err != nil {
		return false, err
	}

	isNmstateServiceSupported, err := s.NMStatectlServiceSupported(openshiftVersion)
	if err != nil {
		return false, err
	}
	return isNmstateServiceSupported && !includesMACIdentifier && !includeAutoDnsSetToFalse, nil
}
