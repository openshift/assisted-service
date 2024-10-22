package nodefeaturediscovery

import (
	"bytes"
	"text/template"
)

// Manifests returns manifests needed to deploy the node feature discovery operator.
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
		"50_nodefeaturediscovery_namespace.yaml":     namespaceManifest,
		"50_nodefeaturediscovery_operatorgroup.yaml": operatorGroupManifest,
		"50_nodefeaturediscovery_subscription.yaml":  operatorSubscriptionManifest,
	}

	nodeFeatureDiscoveryManifest, err := getNodeFeatureDiscovery()
	if err != nil {
		return
	}

	return openshiftManifests, nodeFeatureDiscoveryManifest, nil
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

func getNodeFeatureDiscovery() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": Operator.Namespace,
	}
	return executeTemplate(data, nodeFeatureDiscoveryManifestTemplate)
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
`

const operatorSubscriptionManifestTemplate = `
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  namespace: "{{.OPERATOR_NAMESPACE}}"
  name: "{{.OPERATOR_SUBSCRIPTION_NAME}}"
spec:
  name: nfd
  sourceNamespace: openshift-marketplace
  source: redhat-operators
  channel: stable
  installPlanApproval: Automatic
`

const operatorGroupManifestTemplate = `
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  namespace: "{{.OPERATOR_NAMESPACE}}"
  name: openshift-nfd
spec:
  targetNamespaces:
  - {{.OPERATOR_NAMESPACE}}"
  upgradeStrategy: Default
`

const nodeFeatureDiscoveryManifestTemplate = `
apiVersion: nfd.openshift.io/v1
kind: NodeFeatureDiscovery
metadata:
  namespace: "{{.OPERATOR_NAMESPACE}}"
  name: nfd-instance
spec:
  instance: ""
  operand:
    image: registry.redhat.io/openshift4/ose-node-feature-discovery-rhel9@sha256:e9d3814585bd41b9d0e8612f053dc9753a47c1eae49c07d81f8946e1e25a2fd7
    imagePullPolicy: IfNotPresent
    servicePort: 12000
  topologyUpdater: false
  workerConfig:
    configData: |
      core:
        sleepInterval: 60s
      sources:
        pci:
          deviceClassWhitelist:
          - "0200"
          - "03"
          - "12"
          deviceLabelFields:
          - "vendor"
`
