package ignition

import (
	"encoding/json"

	ignition_config_types_32 "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/openshift/assisted-service/internal/common"
	iccignition "github.com/openshift/image-customization-controller/pkg/ignition"
)

type IronicIgniotionBuilder interface {
	GenerateIronicConfig(ironicBaseURL string, infraEnv common.InfraEnv, ironicAgentImage string) ([]byte, error)
}

type IronicIgniotionBuilderConfig struct {
	// The default ironic agent image was obtained by running oc adm release info --image-for=ironic-agent  quay.io/openshift-release-dev/ocp-release:4.11.0-fc.0-x86_64
	BaremetalIronicAgentImage string `envconfig:"IRONIC_AGENT_IMAGE" default:"quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:d3f1d4d3cd5fbcf1b9249dd71d01be4b901d337fdc5f8f66569eb71df4d9d446"`
}

type ironicIgniotionBuilder struct {
	config IronicIgniotionBuilderConfig
}

func NewIronicIgniotionBuilder(config IronicIgniotionBuilderConfig) IronicIgniotionBuilder {
	ib := ironicIgniotionBuilder{config}
	return &ib
}

func (r *ironicIgniotionBuilder) GenerateIronicConfig(ironicBaseURL string, infraEnv common.InfraEnv, ironicAgentImage string) ([]byte, error) {
	config := ignition_config_types_32.Config{}
	config.Ignition.Version = "3.2.0"

	httpProxy, httpsProxy, noProxy := common.GetProxyConfigs(infraEnv.Proxy)
	if ironicAgentImage == "" {
		ironicAgentImage = r.config.BaremetalIronicAgentImage
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
