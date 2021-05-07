package ocs

type ocsDeploymentMode string

const (
	// Aggregate CPU for compact mode is 24 and 2 CPU for each disk
	// so per host requires (24-(2*3))/3 = 6 CPU per host
	CPUCompactMode int64 = 6

	// Aggregate CPU for minimal mode is 18 and 2 CPU for each disk
	// so per host requires (18-(2*3))/3 = 4 CPU per host
	CPUMinimalMode int64 = 4

	// Aggregate Memory for compact mode is 72 and 5 GiB for each disk
	// so per host requires (72-(5*3))/3 = 19 GiB RAM per host
	MemoryGiBCompactMode int64 = 19

	// Aggregate Memory for minimal mode is 72 and 5 GiB for each disk
	// so per host requires (72-(5*3))/3 = 19 GiB RAM per host
	MemoryGiBMinimalMode int64 = 19

	ssdDrive     string            = "SSD"
	hddDrive     string            = "HDD"
	MinDiskSize  int64             = 5 //5GB is the min disk size for OCS
	compactMode  ocsDeploymentMode = "Compact"
	minimalMode  ocsDeploymentMode = "Minimal"
	standardMode ocsDeploymentMode = "Standard"
)
