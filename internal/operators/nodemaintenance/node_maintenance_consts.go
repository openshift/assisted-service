package nodemaintenance

import "github.com/openshift/assisted-service/models"

const (
	operatorName             = "node-maintenance"
	operatorSubscriptionName = "node-maintenance-operator"
	operatorNamespace        = "openshift-workload-maintenance"
	OperatorFullName         = "Node Maintenance Operator"

	clusterValidationID = string(models.ClusterValidationIDNodeMaintenanceRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDNodeMaintenanceRequirementsSatisfied)
)
