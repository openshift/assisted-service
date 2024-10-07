package pipelines

import (
	"bytes"
	"text/template"
)

// Manifests returns manifests needed to deploy the pipeline operator.
func Manifests() (openshiftManifests map[string][]byte, customManifests []byte, err error) {
	operatorSubscriptionManifest, err := getSubscription()
	if err != nil {
		return
	}

	openshiftManifests = map[string][]byte{
		"50_pipelines_subscription.yaml": operatorSubscriptionManifest,
	}

	return openshiftManifests, nil, nil
}

func getSubscription() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         Operator.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME": Operator.SubscriptionName,
	}
	return executeTemplate(data, operatorSubscriptionManifestTemplate)
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

const operatorSubscriptionManifestTemplate = `
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  namespace: "{{.OPERATOR_NAMESPACE}}"
  name: "{{.OPERATOR_SUBSCRIPTION_NAME}}"
spec:
  name: openshift-pipelines-operator-rh
  sourceNamespace: openshift-marketplace
  source: redhat-operators
  channel: latest
  installPlanApproval: Automatic
`
