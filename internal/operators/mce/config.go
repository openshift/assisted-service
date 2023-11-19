package mce

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/openshift/assisted-service/internal/common"
)

type EnvironmentalConfig struct {
	OCPMCEVersionMap string `envconfig:"OCP_MCE_VERSION_MAP" default:"[{\"openshift_version\": \"4.11\", \"mce_channel\": \"stable-2.3\"}, {\"openshift_version\": \"4.12\", \"mce_channel\": \"stable-2.4\"}, {\"openshift_version\": \"4.14\", \"mce_channel\": \"stable-2.4\"}, {\"openshift_version\": \"4.14\", \"mce_channel\": \"stable-2.4\"}, {\"openshift_version\": \"4.15\", \"mce_channel\": \"stable-2.4\"}]"`
}

type OcpMceVersionMap struct {
	OpenshiftVersion string `json:"openshift_version"`
	MceChannel       string `json:"mce_channel"`
}

type Config struct {
	OcpMceVersionMap []OcpMceVersionMap
}

const (
	// Memory value provided in GiB
	MinimumMemory int64 = 16
	MinimumCPU    int64 = 4

	// Memory value provided in GiB
	SNOMinimumMemory int64 = 32
	SNOMinimumCpu    int64 = 8
)

func getMinMceOpenshiftVersion(ocpMceVersionMap []OcpMceVersionMap) (*string, error) {
	lowestVersion := ocpMceVersionMap[0].OpenshiftVersion
	for _, version := range ocpMceVersionMap {
		isLower, err := common.BaseVersionLessThan(lowestVersion, version.OpenshiftVersion)
		if err != nil {
			return nil, err
		}
		if isLower {
			lowestVersion = version.OpenshiftVersion
		}
	}

	return &lowestVersion, nil
}

func parseMCEConfig(config EnvironmentalConfig) (*Config, error) {
	var parsedConfig []OcpMceVersionMap
	if err := json.Unmarshal([]byte(config.OCPMCEVersionMap), &parsedConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ocp mce release config: %w", err)
	}

	return &Config{
		OcpMceVersionMap: parsedConfig,
	}, nil
}

func getMCEVersion(openshiftVersion string, ocpMceVersionMap []OcpMceVersionMap) (*string, error) {
	baseVersion, err := common.GetMajorMinorVersion(openshiftVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to get base version of %s", openshiftVersion)
	}

	var mceChannel string
	for _, record := range ocpMceVersionMap {
		if record.OpenshiftVersion == *baseVersion {
			mceChannel = record.MceChannel
		}
	}

	if mceChannel == "" {
		return nil, fmt.Errorf("failed to find mce channel for the given openshift version %s", openshiftVersion)
	}

	return &mceChannel, nil
}
