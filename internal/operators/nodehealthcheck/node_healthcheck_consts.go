package nodehealthcheck

import "github.com/openshift/assisted-service/models"

const (
	operatorSubscriptionName = "node-healthcheck-operator"
	operatorNamespace        = "openshift-workload-availability"
	OperatorFullName         = "Node Healthcheck"

	clusterValidationID = string(models.ClusterValidationIDNodeHealthcheckRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDNodeHealthcheckRequirementsSatisfied)
)
