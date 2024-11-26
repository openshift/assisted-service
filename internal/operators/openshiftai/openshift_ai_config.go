package openshiftai

// These requirements have been extracted from this document:
//
//	https://docs.redhat.com/en/documentation/red_hat_openshift_ai_self-managed/2.13/html/installing_and_uninstalling_openshift_ai_self-managed/installing-and-deploying-openshift-ai_install#installing-and-deploying-openshift-ai_install
type Config struct {
	MinWorkerNodes     int64 `envconfig:"OPENSHIFT_AI_MIN_WORKER_NODES" default:"2"`
	MinWorkerMemoryGiB int64 `envconfig:"OPENSHIFT_AI_MIN_WORKER_MEMORY_GIB" default:"32"`
	MinWorkerCPUCores  int64 `envconfig:"OPENSHIFT_AI_MIN_WORKER_CPU_CORES" default:"8"`
	RequireGPU         bool  `envconfig:"OPENSHIFT_AI_REQUIRE_GPU" default:"true"`

	// TODO: Currently we use the controller image to run the setup tools because all we need is the shell and the
	// `oc` command, and that way we don't need an additional image. But in the future we will probably want to have
	// a separate image that contains the things that we need to run these setup jobs.
	ControllerImage string `envconfig:"CONTROLLER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer-controller:latest"`

	// SupportedGPUS is a comma separated list of vendor identifiers of supported GPUs. For examaple, to enable
	// support for NVIDIA and Virtio GPUS the value should be `10de,1af4`. By default only NVIDIA GPUs are
	// supported.
	SupportedGPUs []string `envconfig:"OPENSHIFT_AI_SUPPORTED_GPUS" default:"10de"`
}
