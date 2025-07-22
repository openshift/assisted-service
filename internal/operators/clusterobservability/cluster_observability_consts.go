package clusterobservability

import "github.com/openshift/assisted-service/models"

const (
	operatorName             = "cluster-observability"
	operatorSubscriptionName = "cluster-observability-operator"
	operatorNamespace        = "openshift-cluster-observability-operator"
	OperatorFullName         = "Cluster Observability"

	clusterValidationID = string(models.ClusterValidationIDClusterObservabilityRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDClusterObservabilityRequirementsSatisfied)
)
