package mce

const (
	MceMinOpenshiftVersion string = "4.10.0"
	MceChannelFormat       string = "stable-2.%d"

	// Memory value provided in GiB
	MinimumMemory int64 = 16
	MinimumCPU    int64 = 4

	// Memory value provided in GiB
	SNOMinimumMemory int64 = 32
	SNOMinimumCpu    int64 = 8
)

type Config struct {
	MceMinOpenshiftVersion string `envconfig:"MCE_MIN_OPENSHIFT_VERSION" default:"4.10.0"`
}
