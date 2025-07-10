package metallb

const (
	MetalLBMinOpenshiftVersion string = "4.11.0"
	MetalLBNamespace           string = "metallb-system"
	MetalLBSubscriptionName    string = "metallb-operator"
)

type Config struct {
	MetalLBCPUPerHost       int64 `envconfig:"METALLB_CPU_PER_HOST" default:"1"`
	MetalLBMemoryPerHostMiB int64 `envconfig:"METALLB_MEMORY_PER_HOST_MIB" default:"100"`
}

type Properties struct {
	ApiIP     string `json:"api_ip"`
	IngressIP string `json:"ingress_ip"`
}
