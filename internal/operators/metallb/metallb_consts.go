package metallb

import "github.com/openshift/assisted-service/models"

const (
	operatorName             = "metallb"
	operatorSubscriptionName = "metallb-operator"
	operatorNamespace        = "metallb-system"
	OperatorFullName         = "MetalLB"

	clusterValidationID = string(models.ClusterValidationIDMetallbRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDMetallbRequirementsSatisfied)
)
