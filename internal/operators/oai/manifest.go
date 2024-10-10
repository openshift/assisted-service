package oai

import (
	"bytes"
	"text/template"
)

// Manifests returns manifests needed to deploy OpenShift.
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
		"50_openshift-ai_ns.yaml":                    namespaceManifest,
		"50_openshift-ai_operator_group.yaml":        operatorGroupManifest,
		"50_openshift-ai_operator_subscription.yaml": operatorSubscriptionManifest,
	}

	return openshiftManifests, nil, nil
}

func getSubscription() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         operatorNamespace,
		"OPERATOR_SUBSCRIPTION_NAME": subscriptionName,
	}
	return executeTemplate(data, operatorSubscriptionManifestTemplate)
}

func getNamespace() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": operatorNamespace,
	}
	return executeTemplate(data, namespaceManifestTemplate)
}

func getOperatorGroup() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": operatorNamespace,
	}
	return executeTemplate(data, operatorGroupManifestTemplate)
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
  name: rhods-operator
  installPlanApproval: Automatic
`

const operatorGroupManifestTemplate = `
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  namespace: "{{.OPERATOR_NAMESPACE}}"
  name: rhods-operator
spec: {}
`
