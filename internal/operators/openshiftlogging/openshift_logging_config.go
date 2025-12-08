package openshiftlogging

const (
	// OpenShiftLoggingMinOpenshiftVersion is the minimum OpenShift version that supports OpenShift Logging operator
	// Based on operator metadata: com.redhat.openshift.versions: v4.17-v4.20
	OpenShiftLoggingMinOpenshiftVersion = "4.17.0"

	// Operator metadata
	Name             = "openshift-logging"
	FullName         = "Red Hat OpenShift Logging Operator"
	Namespace        = "openshift-logging"
	SubscriptionName = "cluster-logging"
	Source           = "redhat-operators"
	SourceNamespace  = "openshift-marketplace"
	Channel          = "stable-6.3"
)
