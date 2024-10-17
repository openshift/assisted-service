package openshiftai

import (
	"bytes"
	"text/template"
)

// Manifests returns manifests needed to deploy the OpenShift AI operator.
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
		"50_openshift_ai_namespace.yaml":     namespaceManifest,
		"50_openshift_ai_operatorgroup.yaml": operatorGroupManifest,
		"50_openshift_ai_subscription.yaml":  operatorSubscriptionManifest,
	}

	customManifestsBuffer := &bytes.Buffer{}
	dataScienceClusterManifest, err := getDataScienceCluster()
	if err != nil {
		return
	}
	if len(dataScienceClusterManifest) > 0 {
		customManifestsBuffer.WriteString("---\n")
		customManifestsBuffer.Write(dataScienceClusterManifest)
		customManifestsBuffer.WriteString("\n")
	}
	nvidiaAcceleratorProfileManifest, err := getNvidiaAcceleratorProfile()
	if err != nil {
		return
	}
	if len(nvidiaAcceleratorProfileManifest) > 0 {
		customManifestsBuffer.WriteString("---\n")
		customManifestsBuffer.Write(nvidiaAcceleratorProfileManifest)
		customManifestsBuffer.WriteString("\n")
	}
	customManifests = customManifestsBuffer.Bytes()

	return
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

func getDataScienceCluster() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": Operator.Namespace,
	}
	return executeTemplate(data, dataScienceClusterManifestTemplate)
}

func getNvidiaAcceleratorProfile() ([]byte, error) {
	return executeTemplate(nil, nvidiaAcceleratorProfileManifestTemplate)
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
  name: rhods-operator
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
  name: rhods-operator
spec:
  upgradeStrategy: Default
`

const dataScienceClusterManifestTemplate = `
apiVersion: datasciencecluster.opendatahub.io/v1
kind: DataScienceCluster
metadata:
  name: default-dsc
spec:
  components:
    codeflare:
      managementState: Managed
    kserve:
      serving:
        ingressGateway:
          certificate:
            type: OpenshiftDefaultIngress
        managementState: Managed
        name: knative-serving
      managementState: Managed
    trustyai:
      managementState: Removed
    ray:
      managementState: Managed
    kueue:
      managementState: Managed
    workbenches:
      managementState: Managed
    dashboard:
      managementState: Managed
    modelmeshserving:
      managementState: Managed
    datasciencepipelines:
      managementState: Managed
    trainingoperator:
      managementState: Removed
`

const nvidiaAcceleratorProfileManifestTemplate = `
apiVersion: dashboard.opendatahub.io/v1
kind: AcceleratorProfile
metadata:
  namespace: redhat-ods-applications
  name: migrated-gpu
spec:
  displayName: NVIDIA GPU
  enabled: true
  identifier: nvidia.com/gpu
  tolerations:
  - key: nvidia.com/gpu
    operator: Exists
    effect: NoSchedule
`
