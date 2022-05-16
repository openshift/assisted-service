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
	// TODO: MGMT-10375	Get the ironic image from the default release image for the arch
	BaremetalIronicAgentImage string `envconfig:"IRONIC_AGENT_IMAGE" default:"registry.ci.openshift.org/openshift:ironic-agent:latest"`
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
