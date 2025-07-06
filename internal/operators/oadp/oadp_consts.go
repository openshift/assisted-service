package oadp

import "github.com/openshift/assisted-service/models"

const (
	operatorName             = "oadp"
	operatorSubscriptionName = "redhat-oadp-operator"
	operatorNamespace        = "openshift-adp"
	OperatorFullName         = "OADP Operator"

	clusterValidationID = string(models.ClusterValidationIDOadpRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDOadpRequirementsSatisfied)
)
