package ocs

type ocsDeploymentMode string

const (
	SsdDrive     string            = "SSD"
	HddDrive     string            = "HDD"
	compactMode  ocsDeploymentMode = "Compact"
	standardMode ocsDeploymentMode = "Standard"
)
