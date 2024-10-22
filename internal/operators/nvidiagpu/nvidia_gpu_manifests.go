package nvidiagpu

import (
	"bytes"
	"text/template"
)

// Manifests returns manifests needed to deploy the NVIDIA GPU operator.
func Manifests() (openshiftManifests map[string][]byte, customManifests []byte, err error) {
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
		"50_nvidia_gpu_namespace.yaml":     namespaceManifest,
		"50_nvidia_gpu_operatorgroup.yaml": operatorGroupManifest,
		"50_nvidia_gpu_subscription.yaml":  operatorSubscriptionManifest,
	}

	clusterPolicyManifest, err := getClusterPolicy()
	if err != nil {
		return
	}

	return openshiftManifests, clusterPolicyManifest, nil
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

func getClusterPolicy() ([]byte, error) {
	return executeTemplate(nil, clusterPolicyManifestTemplate)
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
  name: gpu-operator-certified
  sourceNamespace: openshift-marketplace
  source: certified-operators
  channel: stable
  installPlanApproval: Automatic
`

const operatorGroupManifestTemplate = `
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  namespace: "{{.OPERATOR_NAMESPACE}}"
  name: nvidia-gpu-operator
spec:
  targetNamespaces:
  - nvidia-gpu-operator
  upgradeStrategy: Default
`

const clusterPolicyManifestTemplate = `
apiVersion: nvidia.com/v1
kind: ClusterPolicy
metadata:
  name: gpu-cluster-policy
spec:
  daemonsets:
    rollingUpdate:
      maxUnavailable: "1"
    updateStrategy: RollingUpdate
  dcgm:
    enabled: true
  dcgmExporter:
    config:
      name: ""
    enabled: true
    serviceMonitor:
      enabled: true
  devicePlugin:
    config:
      default: ""
      name: ""
    enabled: true
    mps:
      root: /run/nvidia/mps
  driver:
    certConfig:
      name: ""
    enabled: true
    kernelModuleConfig:
      name: ""
    licensingConfig:
      configMapName: ""
      nlsEnabled: true
    repoConfig:
      configMapName: ""
    upgradePolicy:
      autoUpgrade: true
      drain:
        deleteEmptyDir: false
        enable: false
        force: false
        timeoutSeconds: 300
      maxParallelUpgrades: 1
      maxUnavailable: 25%
      podDeletion:
        deleteEmptyDir: false
        force: false
        timeoutSeconds: 300
      waitForCompletion:
        timeoutSeconds: 0
    useNvidiaDriverCRD: false
    useOpenKernelModules: false
    virtualTopology:
      config: ""
  gdrcopy:
    enabled: false
  gds:
    enabled: false
  gfd:
    enabled: true
  mig:
    strategy: single
  migManager:
    enabled: true
  nodeStatusExporter:
    enabled: true
  operator:
    defaultRuntime: crio
    initContainer: {}
    runtimeClass: nvidia
    use_ocp_driver_toolkit: true
  sandboxDevicePlugin:
    enabled: true
  sandboxWorkloads:
    defaultWorkload: container
    enabled: false
  toolkit:
    enabled: true
    installDir: /usr/local/nvidia
  validator:
    plugin:
      env:
      - name: WITH_WORKLOAD
        value: "false"
  vfioManager:
    enabled: true
  vgpuDeviceManager:
    enabled: true
  vgpuManager:
    enabled: false
`
