package osc

const (
	Name                    = "osc"
	FullName                = "OpenShift Sandboxed Containers"
	Namespace               = "openshift-sandboxed-containers-operator"
	Subscription            = "osc-operator"
	Source                  = "redhat-operators"
	SourceName              = "osc-operator"
	OscMinOpenshiftVersion  = "4.17.0"
	LayeredImageDeployment  = "true"
	LayeredImageUrl         = "quay.io/fidencio/rhcos-layer/ocp-4.16:tdx-20241003-1327"
	KernelArgs              = "kvm_intel.tdx=1"
	// Memory value provided in GiB
	MasterMemory int64 = 1
	MasterCPU    int64 = 1
	// Memory value provided in GIB
	WorkerMemory int64 = 1
	WorkerCPU    int64 = 1
)

type Config struct {
	OscCPUPerHost       int64 `envconfig:"Osc_CPU_PER_HOST" default:"1"`
	OscMemoryPerHostMiB int64 `envconfig:"Osc_MEMORY_PER_HOST_MIB" default:"1024"`
}
