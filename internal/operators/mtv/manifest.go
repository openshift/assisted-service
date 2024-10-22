package mtv

import (
	"bytes"
	"html/template"
)

func Manifests() (map[string][]byte, []byte, error) {
	mtvSubscription, err := getSubscription(Namespace, Subscription, Source, SourceName)
	if err != nil {
		return nil, nil, err
	}

	mtvNamespace, err := getNamespace(Namespace)
	if err != nil {
		return nil, nil, err
	}

	mtvOperatorGroup, err := getOperatorGroup(Namespace)
	if err != nil {
		return nil, nil, err
	}

	forklistController, err := getController(Namespace)
	if err != nil {
		return nil, nil, err
	}

	openshiftManifests := make(map[string][]byte)

	openshiftManifests["50_openshift-mtv_ns.yaml"] = mtvNamespace
	openshiftManifests["50_openshift-mtv_operator_group.yaml"] = mtvOperatorGroup
	openshiftManifests["50_openshift-mtv_subscription.yaml"] = mtvSubscription

	return openshiftManifests, forklistController, nil
}

func executeTemplate(data map[string]string, contentName, content string) ([]byte, error) {
	tmpl, err := template.New(contentName).Parse(content)
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

func getSubscription(namespace, subscription, source, sourceName string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         namespace,
		"OPERATOR_SUBSCRIPTION_NAME": subscription,
		"OPERATOR_SOURCE":            source,
		"OPERATOR_SOURCE_NAME":       sourceName,
	}
	return executeTemplate(data, "mtvSubscription", mtvSubscription)
}

func getNamespace(namespace string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": namespace,
	}
	return executeTemplate(data, "mtvNamespace", mtvNamespace)
}

func getOperatorGroup(namespace string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": namespace,
	}
	return executeTemplate(data, "mtvGroup", mtvGroup)
}

func getController(namespace string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": namespace,
	}
	return executeTemplate(data, "forkliftController", forkliftController)
}

const mtvNamespace = `
apiVersion: v1
kind: Namespace
metadata:
  name: {{.OPERATOR_NAMESPACE}}
`

const mtvGroup = `
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: migration
  namespace: {{ .OPERATOR_NAMESPACE }}
spec:
  targetNamespaces:
    - {{ .OPERATOR_NAMESPACE }}
`

const mtvSubscription = `
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: {{.OPERATOR_SUBSCRIPTION_NAME}}
  namespace: {{.OPERATOR_NAMESPACE}}
spec:
  source: {{.OPERATOR_SOURCE}}
  sourceNamespace: openshift-marketplace
  name: {{.OPERATOR_SOURCE_NAME}}
  installPlanApproval: Automatic
`

const forkliftController = `
apiVersion: forklift.konveyor.io/v1beta1
kind: ForkliftController
metadata:
  name: forklift-controller
  namespace: {{ .OPERATOR_NAMESPACE }}
spec:
  olm_managed: true
`
