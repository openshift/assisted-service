package osc

import (
	"bytes"
	"html/template"
)

func Manifests() (map[string][]byte, []byte, error) {

	oscNamespaceManifest, err := getNamespace(Namespace)
	if err != nil {
		return nil, nil, err
	}

	oscOperatorGroupManifest, err := getOperatorGroup(Namespace)
	if err != nil {
		return nil, nil, err
	}

	oscSubscriptionManifest, err := getSubscription(Namespace, SubscriptionName, Source, SourceName)
	if err != nil {
		return nil, nil, err
	}

	oscKataConfigManifest, err := getKataConfig(Namespace)
	if err != nil {
	        return nil, nil, err
	}


	openshiftManifests := make(map[string][]byte)

	openshiftManifests["50_openshift-osc_ns.yaml"] = oscNamespaceManifest
	openshiftManifests["50_openshift-osc_operator_group.yaml"] = oscOperatorGroupManifest
	openshiftManifests["50_openshift-osc_subscription.yaml"] = oscSubscriptionManifest

	return openshiftManifests, oscKataConfigManifest, err
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
	return executeTemplate(data, "oscSubscriptionManifest", oscSubscriptionManifest)
}

func getNamespace(namespace string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": namespace,
	}
	return executeTemplate(data, "oscNamespaceManifest", oscNamespaceManifest)
}

func getOperatorGroup(namespace string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": namespace,
	}
	return executeTemplate(data, "oscOperatorGroupManifest", oscOperatorGroupManifest)
}

func getKataConfig(namespace string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": namespace,
	}
        return executeTemplate(data, "oscKataConfigManifest", oscKataConfigManifest)
}

const oscNamespaceManifest = `
apiVersion: v1
kind: Namespace
metadata:
  name: {{.OPERATOR_NAMESPACE}}
`

const oscOperatorGroupManifest = `
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: sandboxed-containers-operator-group
  namespace: {{ .OPERATOR_NAMESPACE }}
spec:
  targetNamespaces:
    - {{ .OPERATOR_NAMESPACE }}
`

const oscSubscriptionManifest = `
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

const oscKataConfigManifest = `
apiVersion: kataconfiguration.openshift.io/v1
kind: KataConfig
metadata:
  name: cluster-kataconfig
spec:
  enablePeerPods: false
  logLevel: info
`
