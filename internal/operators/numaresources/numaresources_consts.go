package numaresources

import "github.com/openshift/assisted-service/models"

const (
	operatorName             = "numaresources"
	operatorSubscriptionName = "numaresources-operator"
	operatorNamespace        = "openshift-numaresources"
	OperatorFullName         = "NUMA Resources"

	clusterValidationID = string(models.ClusterValidationIDNumaResourcesRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDNumaResourcesRequirementsSatisfied)
)
