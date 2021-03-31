package ocs

type ocsDeploymentMode string

const (
	CPU          int64             = 8     // per host requires 8 cpus for OCS
	Memory       int64             = 68665 // Memory value provided in MiB for per host (24 GB)
	ssdDrive     string            = "SSD"
	hddDrive     string            = "HDD"
	minDiskSize  int64             = 5 //5GB is the min disk size for OCS
	compactMode  ocsDeploymentMode = "Compact"
	minimalMode  ocsDeploymentMode = "Minimal"
	standardMode ocsDeploymentMode = "Standard"
)
