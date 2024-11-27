package odf

type Config struct {
	ODFNumMinimumDisks              int64 `envconfig:"ODF_NUM_MINIMUM_DISK" default:"3"`
	ODFPerDiskCPUCount              int64 `envconfig:"ODF_PER_DISK_CPU_COUNT" default:"2"` // each disk requires 2 cpus
	ODFPerDiskRAMGiB                int64 `envconfig:"ODF_PER_DISK_RAM_GIB" default:"5"`   // each disk requires 5GiB ram
	ODFNumMinimumHosts              int64 `envconfig:"ODF_NUM_MINIMUM_HOST" default:"3"`
	ODFPerHostCPUCompactMode        int64 `envconfig:"ODF_PER_HOST_CPU_COMPACT_MODE" default:"6"`
	ODFPerHostMemoryGiBCompactMode  int64 `envconfig:"ODF_PER_HOST_MEMORY_GIB_COMPACT_MODE" default:"19"`
	ODFPerHostCPUStandardMode       int64 `envconfig:"ODF_PER_HOST_CPU_STANDARD_MODE" default:"8"`
	ODFPerHostMemoryGiBStandardMode int64 `envconfig:"ODF_PER_HOST_MEMORY_GIB_STANDARD_MODE" default:"19"`
	ODFMinDiskSizeGB                int64 `envconfig:"ODF_MIN_DISK_SIZE_GB" default:"25"`
}
