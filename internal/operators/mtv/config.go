package mtv

const (
	Name                   = "mtv"
	FullName               = "OpenShift Migration Toolkit for Virtualization"
	Namespace              = "openshift-mtv"
	Subscription           = "mtv-operator"
	Source                 = "redhat-operators"
	SourceName             = "mtv-operator"
	MtvMinOpenshiftVersion = "4.14.0"

	// Memory value provided in GiB
	MasterMemory int64 = 1
	MasterCPU    int64 = 1
	// Memory value provided in GIB
	WorkerMemory int64 = 1
	WorkerCPU    int64 = 1
)

type Config struct {
	MtvCPUPerHost       int64 `envconfig:"MTV_CPU_PER_HOST" default:"1"`
	MtvMemoryPerHostMiB int64 `envconfig:"MTV_MEMORY_PER_HOST_MIB" default:"1024"`
}
