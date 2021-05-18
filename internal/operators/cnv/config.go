package cnv

import (
	"strings"
)

type DeviceIDDecoder map[string]bool

type Config struct {
	// List of supported GPUs: https://issues.redhat.com/browse/CNV-7749
	SupportedGPUs DeviceIDDecoder `envconfig:"CNV_SUPPORTED_GPUS" default:"10de:1db6,10de:1eb8"`
	// List of supported SR-IOV NICs: https://docs.openshift.com/container-platform/4.7/networking/hardware_networks/about-sriov.html#supported-devices_about-sriov
	SupportedSRIOVNetworkIC DeviceIDDecoder `envconfig:"CNV_SUPPORTED_SRIOV_NICS" default:"8086:158b,15b3:1015,15b3:1017,15b3:1013,15b3:101b"`
	// CNV operator mode. It defines whether to use upstream `false` or downstream `true`
	Mode bool `envconfig:"CNV_MODE" default:"true"`
}

func (d *DeviceIDDecoder) Decode(value string) error {
	deviceIDSet := make(DeviceIDDecoder)
	*d = deviceIDSet

	if strings.TrimSpace(value) == "" {
		return nil
	}
	devices := strings.Split(value, ",")

	for _, device := range devices {
		deviceIDSet[strings.ToLower(device)] = true
	}
	return nil
}
