package osc

import (
	"bytes"
	"html/template"
)

func Manifests() (map[string][]byte, []byte, error) {

	oscNamespace, err := getNamespace(Namespace)
	if err != nil {
		return nil, nil, err
	}

	oscOperatorGroup, err := getOperatorGroup(Namespace)
	if err != nil {
		return nil, nil, err
	}

	oscSubscription, err := getSubscription(Namespace, Subscription, Source, SourceName)
	if err != nil {
		return nil, nil, err
	}

	oscConfigMap, err := getConfigMap(Namespace, LayeredImageDeployment)
	if err != nil {
		return nil, nil, err
	}

	openshiftManifests := make(map[string][]byte)

	openshiftManifests["50_openshift-osc_ns.yaml"] = oscNamespace
	openshiftManifests["50_openshift-osc_operator_group.yaml"] = oscOperatorGroup
	openshiftManifests["50_openshift-osc_subscription.yaml"] = oscSubscription

	return openshiftManifests, oscConfigMap, nil
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
	return executeTemplate(data, "oscSubscription", oscSubscription)
}

func getNamespace(namespace string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": namespace,
	}
	return executeTemplate(data, "oscNamespace", oscNamespace)
}

func getOperatorGroup(namespace string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": namespace,
	}
	return executeTemplate(data, "oscGroup", oscGroup)
}

func getConfigMap(namespace string, isLayeredImageDeployment string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":                   namespace,
		"OPERATOR_IS_LAYERED_IMAGE_DEPLOYMENT": isLayeredImageDeployment,
	}

	if configMap, err := executeTemplate(data, "oscConfigMap", oscConfigMap); err != nil {
		return nil, err
	} else {
		if isLayeredImageDeployment == "true" {
			LayeredImageDeploymentConfigMap, err := getLayeredImageDeployConfigMap(namespace)
			if err != nil {
				return nil, err
			}
			configMap = append(configMap, []byte("\n---\n")...)
			configMap = append(configMap, LayeredImageDeploymentConfigMap...)

		}
		return configMap, nil
	}
}

func getLayeredImageDeployConfigMap(namespace string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":    namespace,
		"OPERATOR_KERNEL_ARGS":  KernelArgs,
		"OPERATOR_OS_IMAGE_URL": LayeredImageUrl,
	}
	return executeTemplate(data, "oscLayeredImageDeployConfigMap", oscLayeredImageDeployConfigMap)
}

const oscNamespace = `
apiVersion: v1
kind: Namespace
metadata:
  name: {{.OPERATOR_NAMESPACE}}
`

const oscGroup = `
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: sandboxed-containers-operator-group
  namespace: {{ .OPERATOR_NAMESPACE }}
spec:
  targetNamespaces:
    - {{ .OPERATOR_NAMESPACE }}
`

const oscSubscription = `
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
  startingCSV: startingCSV: sandboxed-containers-operator.v1.7.0
`
const oscConfigMap = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: osc-feature-gates
  namespace: {{.OPERATOR_NAMESPACE}}
data:
  # layeredImageDeployment allows deploying Kata using RHCOS layered image
  # This feature gate needs a ConfigMap named layered-image-deploy-cm
  layeredImageDeployment: {{.OPERATOR_IS_LAYERED_IMAGE_DEPLOYMENT}}
`
const oscLayeredImageDeployConfigMap = `
apiVersion: v1
data:
  osImageURL: {{.OPERATOR_OS_IMAGE_URL}}
  kernelArguments: {{.OPERATOR_KERNEL_ARGS}}
kind: ConfigMap
metadata:
  name: layered-image-deploy-cm
  namespace: {{.OPERATOR_NAMESPACE}}
`
