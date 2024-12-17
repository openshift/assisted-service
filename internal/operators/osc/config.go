package osc

const (
	Name                   = "osc"
	FullName               = "OpenShift sandboxed containers"
	Namespace              = "openshift-sandboxed-containers-operator"
	SubscriptionName       = "sandboxed-containers-operator"
	Source                 = "redhat-operators"
	SourceName             = "sandboxed-containers-operator"
	OscMinOpenshiftVersion = "4.10.0"
	// Memory value provided in GiB
	MasterMemory int64 = 1
	MasterCPU    int64 = 1
)

type Config struct {
	OscCPUPerHost       int64 `envconfig:"Osc_CPU_PER_HOST" default:"1"`
	OscMemoryPerHostMiB int64 `envconfig:"Osc_MEMORY_PER_HOST_MIB" default:"1024"`
}
