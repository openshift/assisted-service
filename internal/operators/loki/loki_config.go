package loki

const (
	// LokiMinOpenshiftVersion is the minimum OpenShift version that supports Loki operator
	// Based on operator metadata: com.redhat.openshift.versions: v4.17-v4.20
	LokiMinOpenshiftVersion = "4.17.0"

	// Operator metadata
	Name             = "loki"
	FullName         = "Loki Operator"
	Namespace        = "openshift-loki"
	SubscriptionName = "loki-operator"
	Source           = "redhat-operators"
	SourceNamespace  = "openshift-marketplace"
	Channel          = "stable-6.3"
)
