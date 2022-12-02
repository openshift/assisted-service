package ignition

import (
	"encoding/json"

	ignition_config_types_32 "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/openshift/assisted-service/internal/common"
	iccignition "github.com/openshift/image-customization-controller/pkg/ignition"
)

type IronicIgnitionBuilder interface {
	GenerateIronicConfig(ironicBaseURL string, infraEnv common.InfraEnv, ironicAgentImage string) ([]byte, error)
}

type IronicIgnitionBuilderConfig struct {
	// The default ironic agent image was obtained by running "oc adm release info --image-for=ironic-agent  quay.io/openshift-release-dev/ocp-release:4.11.0-fc.0-x86_64"
	BaremetalIronicAgentImage string `envconfig:"IRONIC_AGENT_IMAGE" default:"quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:d3f1d4d3cd5fbcf1b9249dd71d01be4b901d337fdc5f8f66569eb71df4d9d446"`
	// The default ironic agent image for arm architecture was obtained by running "oc adm release info --image-for=ironic-agent quay.io/openshift-release-dev/ocp-release@sha256:1b8e71b9bccc69c732812ebf2bfba62af6de77378f8329c8fec10b63a0dbc33c"
	// The release image digest for arm architecture was obtained from this link https://mirror.openshift.com/pub/openshift-v4/aarch64/clients/ocp-dev-preview/4.11.0-fc.0/release.txt
	BaremetalIronicAgentImageForArm string `envconfig:"IRONIC_AGENT_IMAGE_ARM" default:"quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:cb0edf19fffc17f542a7efae76939b1e9757dc75782d4727fb0aa77ed5809b43"`
}

type ironicIgnitionBuilder struct {
	config IronicIgnitionBuilderConfig
}

func NewIronicIgnitionBuilder(config IronicIgnitionBuilderConfig) IronicIgnitionBuilder {
	ib := ironicIgnitionBuilder{config}
	return &ib
}

func (r *ironicIgnitionBuilder) GenerateIronicConfig(ironicBaseURL string, infraEnv common.InfraEnv, ironicAgentImage string) ([]byte, error) {
	config := ignition_config_types_32.Config{}
	config.Ignition.Version = "3.2.0"

	httpProxy, httpsProxy, noProxy := common.GetProxyConfigs(infraEnv.Proxy)
	if ironicAgentImage == "" {
		// if ironicAgentImage wasn't specified use the default image for the CPU arch
		if infraEnv.CPUArchitecture == common.ARM64CPUArchitecture {
			ironicAgentImage = r.config.BaremetalIronicAgentImageForArm
		} else {
			ironicAgentImage = r.config.BaremetalIronicAgentImage
		}
	}
	// TODO: this should probably get the pullSecret as well
	ib, err := iccignition.New([]byte{}, []byte{}, ironicBaseURL, ironicAgentImage, "", "", "", httpProxy, httpsProxy, noProxy, "")
	if err != nil {
		return []byte{}, err
	}
	config.Storage.Files = []ignition_config_types_32.File{ib.IronicAgentConf()}
	// TODO: sort out the flags (authfile...) and copy network
	config.Systemd.Units = []ignition_config_types_32.Unit{ib.IronicAgentService(false)}
	return json.Marshal(config)
}
