package mce

import (
	"bytes"
	"text/template"
)

// Manifests returns manifests needed to deploy MCE.
func Manifests() (openshiftManifests map[string][]byte, customManifests []byte, err error) {
	// Generate the OpenShift manifests:
	namespaceManifest, err := getNamespace()
	if err != nil {
		return
	}
	operatorGroupManifest, err := getOperatorGroup()
	if err != nil {
		return
	}
	operatorSubscriptionManifest, err := getSubscription()
	if err != nil {
		return
	}
	clusterEngineManifest, err := getClusterEngine()
	if err != nil {
		return
	}
	localClusterManifest, err := getLocalCluster()
	if err != nil {
		return
	}

	openshiftManifests = map[string][]byte{
		"50_openshift-mce_ns.yaml":                    namespaceManifest,
		"50_openshift-mce_operator_group.yaml":        operatorGroupManifest,
		"50_openshift-mce_operator_subscription.yaml": operatorSubscriptionManifest,
	}
	var customManifestsBuffer bytes.Buffer
	customManifestsBuffer.Write(clusterEngineManifest)
	customManifestsBuffer.WriteString("\n---\n")
	customManifestsBuffer.Write(localClusterManifest)
	customManifests = customManifestsBuffer.Bytes()

	return
}

func getSubscription() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         monitoredOperator.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME": monitoredOperator.SubscriptionName,
	}
	return executeTemplate(data, operatorSubscriptionManifestTemplate)
}

func getNamespace() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": monitoredOperator.Namespace,
	}
	return executeTemplate(data, namespaceManifestTemplate)
}

func getOperatorGroup() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": monitoredOperator.Namespace,
	}
	return executeTemplate(data, operatorGroupManifestTemplate)
}

func getClusterEngine() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": monitoredOperator.Namespace,
	}
	return executeTemplate(data, clusterEngineManifestTemplate)
}

func getLocalCluster() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": monitoredOperator.Namespace,
	}
	return executeTemplate(data, localClusterManifestTemplate)
}

func executeTemplate(data map[string]string, content string) ([]byte, error) {
	tmpl, err := template.New("").Parse(content)
	if err != nil {
		return nil, err
	}
	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, data)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const namespaceManifestTemplate = `
apiVersion: v1
kind: Namespace
metadata:
  name: {{ .OPERATOR_NAMESPACE }}
spec: {}
`

const operatorSubscriptionManifestTemplate = `
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  namespace: "{{ .OPERATOR_NAMESPACE }}"
  name: "{{ .OPERATOR_SUBSCRIPTION_NAME }}"
spec:
  sourceNamespace: openshift-marketplace
  source: redhat-operators
  name: multicluster-engine
  installPlanApproval: Automatic
`

const operatorGroupManifestTemplate = `
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  namespace: "{{ .OPERATOR_NAMESPACE }}"
  name: mce
spec:
  targetNamespaces:
  - "{{.OPERATOR_NAMESPACE}}"
  `

const clusterEngineManifestTemplate = `
apiVersion: multicluster.openshift.io/v1
kind: MultiClusterEngine
metadata:
  name: mce
spec: {}
`

const localClusterManifestTemplate = `
apiVersion: cluster.open-cluster-management.io/v1
kind: ManagedCluster
metadata:
  namespace: "{{ .OPERATOR_NAMESPACE }}"
  name: local-cluster
  labels:
    local-cluster: "true"
    cloud: auto-detect
    vendor: auto-detect
spec:
  hubAcceptsClient: true
`
