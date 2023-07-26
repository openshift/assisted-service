package metallb

type Config struct {
	MetalLBMinOpenshiftVersion string `envconfig:"METALLB_MIN_OPENSHIFT_VERSION" default:"4.11.0"`
}

type Properties struct {
	ApiIP     string `json:"api_ip"`
	IngressIP string `json:"ingress_ip"`
}
