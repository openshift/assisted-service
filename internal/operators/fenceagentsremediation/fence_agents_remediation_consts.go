package fenceagentsremediation

import "github.com/openshift/assisted-service/models"

const (
	operatorName             = "fence-agents-remediation"
	operatorSubscriptionName = "fence-agents-remediation"
	operatorNamespace        = "openshift-workload-availability"
	OperatorFullName         = "Fence Agents Remediation"

	clusterValidationID = string(models.ClusterValidationIDFenceAgentsRemediationRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDFenceAgentsRemediationRequirementsSatisfied)
)
