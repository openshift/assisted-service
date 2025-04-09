package amdgpu

type Config struct {
	// SupportedGPUS is a comma separated list of vendor identifiers of supported GPUs. For example, to enable
	// support for AMD and Virtio GPUS the value should be `1002,1af4`. By default only AMD GPUs are supported.
	SupportedGPUs []string `envconfig:"AMD_SUPPORTED_GPUS" default:"1002"`
}
