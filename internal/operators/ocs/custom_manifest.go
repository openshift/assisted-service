package ocs

import (
	"bytes"
	"text/template"
)

// Subscription Manifest for testing internal builds
func ocsCustomSubscription(channel string) (string, error) {

	data := map[string]string{
		"OPERATOR_NAMESPACE":         Operator.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME": Operator.SubscriptionName,
		"OPERATOR_CHANNEL":           channel,
	}

	const ocsSubscription = `apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: "{{.OPERATOR_SUBSCRIPTION_NAME}}"
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  installPlanApproval: Automatic
  name: ocs-operator
  channel: {{.OPERATOR_CHANNEL}}
  source: ocs-catalogsource
  sourceNamespace: openshift-marketplace`

	tmpl, err := template.New("ocsSubscription").Parse(ocsSubscription)
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, data)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// CatalogSource Manifest for testing internal builds
func ocsCatalogSource(image string) (string, error) {
	data := map[string]string{
		"OPERATOR_IMAGE": image,
	}

	const CatalogSource = `apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ocs-catalogsource
  namespace: openshift-marketplace
  labels:
      ocs-operator-internal: "true"
spec:
  displayName: Openshift Container Storage
  icon:
    base64data: ""
    mediatype: ""
  image: "{{.OPERATOR_IMAGE}}"
  publisher: Red Hat
  sourceType: grpc`

	tmpl, err := template.New("CatalogSource").Parse(CatalogSource)
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, data)
	if err != nil {
		return "", err
	}
	return buf.String(), nil

}
