package ignition

import (
	"encoding/json"

	ignition_config_types_32 "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/openshift/assisted-service/internal/common"
	iccignition "github.com/openshift/image-customization-controller/pkg/ignition"
)

func GenerateIronicConfig(ironicBaseURL string, infraEnv common.InfraEnv, ironicAgentImage string) ([]byte, error) {
	config := ignition_config_types_32.Config{}
	config.Ignition.Version = "3.2.0"

	httpProxy, httpsProxy, noProxy := common.GetProxyConfigs(infraEnv.Proxy)
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
