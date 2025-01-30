package nmstate

import (
	"bytes"
	"html/template"
)

func Manifests() (map[string][]byte, []byte, error) {

	nmstateNamespaceManifest, err := getNamespace(Namespace)
	if err != nil {
		return nil, nil, err
	}

	nmstateOperatorGroupManifest, err := getOperatorGroup(Namespace, GroupName)
	if err != nil {
		return nil, nil, err
	}

	nmstateSubscriptionManifest, err := getSubscription(Namespace, SubscriptionName, Source, SourceName)
	if err != nil {
		return nil, nil, err
	}

	openshiftManifests := make(map[string][]byte)

	openshiftManifests["50_openshift-nmstate_ns.yaml"] = nmstateNamespaceManifest
	openshiftManifests["50_openshift-nmstate_operator_group.yaml"] = nmstateOperatorGroupManifest
	openshiftManifests["50_openshift-nmstate_subscription.yaml"] = nmstateSubscriptionManifest

	return openshiftManifests, []byte(nmstateControllerManifest), err
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
	return executeTemplate(data, "nmstateSubscriptionManifest", nmstateSubscriptionManifest)
}

func getNamespace(namespace string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": namespace,
	}
	return executeTemplate(data, "nmstateNamespaceManifest", nmstateNamespaceManifest)
}

func getOperatorGroup(namespace string, groupName string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":  namespace,
		"OPERATOR_GROUP_NAME": groupName,
	}
	return executeTemplate(data, "nmstateOperatorGroupManifest", nmstateOperatorGroupManifest)
}

const nmstateNamespaceManifest = `
apiVersion: v1
kind: Namespace
metadata:
  name: {{.OPERATOR_NAMESPACE}}
spec:
  finalizers:
  - kubernetes
`

const nmstateOperatorGroupManifest = `
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: {{ .OPERATOR_GROUP_NAME }}
  namespace: {{ .OPERATOR_NAMESPACE }}
spec:
  targetNamespaces:
    - {{ .OPERATOR_NAMESPACE }}
`

const nmstateSubscriptionManifest = `
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: {{.OPERATOR_SUBSCRIPTION_NAME}}
  namespace: {{.OPERATOR_NAMESPACE}}
spec:
  channel: stable
  source: {{.OPERATOR_SOURCE}}
  sourceNamespace: openshift-marketplace
  name: {{.OPERATOR_SOURCE_NAME}}
  installPlanApproval: Automatic
`

const nmstateControllerManifest = `
apiVersion: nmstate.io/v1
kind: NMState
metadata:
  name: nmstate
`
