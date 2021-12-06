package odf

type odfDeploymentMode string

const (
	SsdDrive     string            = "SSD"
	HddDrive     string            = "HDD"
	compactMode  odfDeploymentMode = "Compact"
	standardMode odfDeploymentMode = "Standard"
)
