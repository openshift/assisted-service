package metallb

type Config struct {
	MetalLBCPUPerHost          int64  `envconfig:"METALLB_CPU_PER_HOST" default:"1"`
	MetalLBMemoryPerHostMiB    int64  `envconfig:"METALLB_MEMORY_PER_HOST_MIB" default:"1200"`
	MetalLBMinOpenshiftVersion string `envconfig:"METALLB_MIN_OPENSHIFT_VERSION" default:"4.11.0"`
}
