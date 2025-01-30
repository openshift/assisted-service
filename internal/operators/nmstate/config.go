package nmstate

const (
	Name                       = "nmstate"
	FullName                   = "Kubernetes NMState Operator"
	Namespace                  = "openshift-nmstate"
	SubscriptionName           = "kubernetes-nmstate-operator"
	Source                     = "redhat-operators"
	SourceName                 = "kubernetes-nmstate-operator"
	GroupName                  = "openshift-nmstate"
	NmstateMinOpenshiftVersion = "4.12.0"

	// Memory value provided in MiB
	MasterMemory int64 = 100
	// TODO: change to 0.3 when float values would be accepted for ClusterHostRequirementsDetails.CPUCores
	MasterCPU int64 = 0
	// Memory value provided in MiB
	WorkerMemory int64 = 100
	// TODO: change to 0.3 when float values would be accepted for ClusterHostRequirementsDetails.CPUCores
	WorkerCPU int64 = 0
)
