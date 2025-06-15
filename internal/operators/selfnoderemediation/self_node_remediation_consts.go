package selfnoderemediation

import "github.com/openshift/assisted-service/models"

const (
	operatorSubscriptionName = "self-node-remediation"
	operatorNamespace        = "openshift-workload-availability"
	OperatorFullName         = "Self Node Remediation"

	clusterValidationID = string(models.ClusterValidationIDSelfNodeRemediationRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDSelfNodeRemediationRequirementsSatisfied)
)
