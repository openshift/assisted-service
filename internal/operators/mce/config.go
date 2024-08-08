package mce

const (
	MceMinOpenshiftVersion string = "4.10.0"

	// Memory value provided in GiB
	MinimumMemory int64 = 16
	MinimumCPU    int64 = 4

	// Memory value provided in GiB
	SNOMinimumMemory int64 = 32
	SNOMinimumCpu    int64 = 8

	// How much we allow the user to deviate from our official
	// minimum. Some setups (e.g. VMs) might give just a bit less
	// than requested, so we shouldn't be too strict about it
	MemoryRequirementToleranceMiB int64 = 100
)

type Config struct {
	MceMinOpenshiftVersion string `envconfig:"MCE_MIN_OPENSHIFT_VERSION" default:"4.10.0"`
}
