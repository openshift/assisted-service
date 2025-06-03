package kubedescheduler

import "github.com/openshift/assisted-service/models"

const (
	operatorName             = "kube-descheduler"
	operatorSubscriptionName = "cluster-kube-descheduler-operator"
	operatorNamespace        = "openshift-kube-descheduler-operator"
	OperatorFullName         = "Kube Descheduler"

	clusterValidationID = string(models.ClusterValidationIDKubeDeschedulerRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDKubeDeschedulerRequirementsSatisfied)
)
