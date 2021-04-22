package ocs

type ocsDeploymentMode string

const (
	// Aggregate CPU for compact mode is 36(including OCP), OCP requires 4 CPU per host on master, and 2 CPU for each disk
	// so per host requires (36-(4*3)-(2*3))/3 = 6 CPU per host
	CPUCompactMode int64 = 6

	// Aggregate CPU for minimal mode is 24(including OCP), OCP requires 2 CPU per host on worker, and 2 CPU for each disk
	// so per host requires (24-(2*3)-(2*3))/3 = 4 CPU per host
	CPUMinimalMode int64 = 4

	// Aggregate Memory for compact mode is 120(including OCP), OCP requires 16 GB per host, and 5 GB for each disk
	// so per host requires (120-(16*3)-(5*3))/3 = 19 GB RAM per host
	MemoryGBCompactMode int64 = 19

	// Aggregate Memory for minimal mode is 72(including OCP), OCP requires 8 GB per host, and 5 GB for each disk
	// so per host requires (72-(8*3)-(5*3))/3 = 11 GB RAM per host
	MemoryGBMinimalMode int64 = 11

	ssdDrive     string            = "SSD"
	hddDrive     string            = "HDD"
	MinDiskSize  int64             = 5 //5GB is the min disk size for OCS
	compactMode  ocsDeploymentMode = "Compact"
	minimalMode  ocsDeploymentMode = "Minimal"
	standardMode ocsDeploymentMode = "Standard"
)
