package ignition

import (
	ignition_config_types_32 "github.com/coreos/ignition/v2/config/v3_2/types"
	"sigs.k8s.io/yaml"
)

type nmstateOutput struct {
	NetworkManager [][]string `yaml:"NetworkManager"`
}

func nmstateOutputToFiles(generatedConfig []byte) ([]ignition_config_types_32.File, error) {
	files := []ignition_config_types_32.File{}

	networkManagerConfig := &nmstateOutput{}
	err := yaml.Unmarshal(generatedConfig, networkManagerConfig)
	if err != nil {
		return nil, err
	}
	if networkManagerConfig.NetworkManager == nil {
		return files, nil
	}
	for _, v := range networkManagerConfig.NetworkManager {
		files = append(files,
			ignitionFileEmbed("/etc/NetworkManager/system-connections/"+v[0],
				0600, true,
				[]byte(v[1])))
	}
	return files, nil
}
