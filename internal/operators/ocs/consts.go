package ocs

type ocsDeploymentMode string

const (
	ssdDrive     string            = "SSD"
	hddDrive     string            = "HDD"
	compactMode  ocsDeploymentMode = "Compact"
	standardMode ocsDeploymentMode = "Standard"
)
