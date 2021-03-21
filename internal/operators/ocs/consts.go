package ocs

type ocsDeploymentMode string

const (
	ssdDrive     string            = "SSD"
	hddDrive     string            = "HDD"
	minDiskSize  int64             = 5 //5GB is the min disk size for OCS
	compactMode  ocsDeploymentMode = "Compact"
	minimalMode  ocsDeploymentMode = "Minimal"
	standardMode ocsDeploymentMode = "Standard"
)
