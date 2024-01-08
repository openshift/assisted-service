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
	openshiftManifests = map[string][]byte{
		"50_openshift-mce_ns.yaml":                    namespaceManifest,
		"50_openshift-mce_operator_group.yaml":        operatorGroupManifest,
		"50_openshift-mce_operator_subscription.yaml": operatorSubscriptionManifest,
	}

	mceManifest, err := getMultiClusterEngine()
	if err != nil {
		return
	}

	return openshiftManifests, mceManifest, nil
}

func getSubscription() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         Operator.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME": Operator.SubscriptionName,
	}
	return executeTemplate(data, operatorSubscriptionManifestTemplate)
}

func getNamespace() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": Operator.Namespace,
	}
	return executeTemplate(data, namespaceManifestTemplate)
}

func getOperatorGroup() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": Operator.Namespace,
	}
	return executeTemplate(data, operatorGroupManifestTemplate)
}

func getMultiClusterEngine() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": Operator.Namespace,
	}
	return executeTemplate(data, clusterEngineManifestTemplate)
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
  name: {{.OPERATOR_NAMESPACE}}
spec: {}
`

const operatorSubscriptionManifestTemplate = `
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  namespace: "{{ .OPERATOR_NAMESPACE }}"
  name: "{{.OPERATOR_SUBSCRIPTION_NAME}}"
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
  namespace: "{{.OPERATOR_NAMESPACE}}"
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
spec:
  targetNamespace: "{{.OPERATOR_NAMESPACE}}"
`
