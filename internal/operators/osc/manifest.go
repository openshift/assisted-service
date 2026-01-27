package osc

import (
	"bytes"
	"fmt"
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

	customManifests, err := generateCustomManifests(Namespace)
	if err != nil {
		return nil, nil, err
	}

	openshiftManifests := make(map[string][]byte)

	openshiftManifests["50_openshift-osc_ns.yaml"] = oscNamespaceManifest
	openshiftManifests["50_openshift-osc_operator_group.yaml"] = oscOperatorGroupManifest
	openshiftManifests["50_openshift-osc_subscription.yaml"] = oscSubscriptionManifest

	return openshiftManifests, customManifests, err
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

// generateCustomManifests generate all custom Manifests and merge them
func generateCustomManifests(namespace string) ([]byte, error) {
	// Define all template data
	templateData := []struct {
		data     map[string]string
		name     string
		template string
	}{
		{map[string]string{"TDX_MC_NAME": "99-enable-intel-tdx"}, "kataccMcManifest", kataccMcManifest},
		{map[string]string{"NFD_NAMESPACE": "openshift-nfd", "NFD_NAME": "nfd-instance"}, "kataccNfdManifest", kataccNfdManifest},
		{map[string]string{"OSC_RULES_NAME": "osc-rules", "NFD_NAMESPACE": "openshift-nfd"}, "oscRulesManifest", oscRulesManifest},
		{map[string]string{"INTEL_NFR_NAME": "intel-dp-devices", "INTE_NFR_NAMESPACE": "openshift-nfd"}, "intelNfrManifest", intelNfrManifest},
		{map[string]string{"AMD_NFR_NAME": "amd-sev-snp", "AMD_NFR_NAMESPACE": "openshift-nfd"}, "amdNfrManifest", amdNfrManifest},
		{map[string]string{"OSC_FEATURE_GATE_NAME": "osc-feature-gates", "OPERATOR_NAMESPACE": namespace}, "oscFeaturegateManifest", oscFeaturegateManifest},
		{map[string]string{"OPERATOR_NAMESPACE": namespace}, "oscKataconfigManifest", oscKataconfigManifest},
	}

	var yamlfiles [][]byte
	// Batch Processing Template
	for _, item := range templateData {
		result, err := executeTemplate(item.data, item.name, item.template)
		if err != nil {
			return nil, fmt.Errorf("failed to generate %s: %w", item.name, err)
		}
		yamlfiles = append(yamlfiles, result)
	}

	// Merge all files with delimiter
	return bytes.Join(yamlfiles, []byte("---\n")), nil

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

const oscKataconfigManifest = `
apiVersion: kataconfiguration.openshift.io/v1
kind: KataConfig
metadata:
  name: cluster-kataconfig
  namespace: {{.OPERATOR_NAMESPACE}}
spec:
  enablePeerPods: false
  logLevel: info
`

const kataccMcManifest = `
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: master
  name: {{.TDX_MC_NAME}}
spec:
  kernelArguments:
  - kvm_intel.tdx=1
  - nohibernate
  config:
    ignition:
      version: 3.2.0
    storage:
      files:
        - path: /etc/modules-load.d/vsock.conf
          mode: 0644
          contents:
            source: data:text/plain;charset=utf-8;base64,dnNvY2stbG9vcGJhY2sK
`

const kataccNfdManifest = `
apiVersion: nfd.openshift.io/v1
kind: NodeFeatureDiscovery
metadata:
  name: {{.NFD_NAME}}
  namespace: {{.NFD_NAMESPACE}}
spec:
  workerConfig:
    configData: |
`

const oscRulesManifest = `
apiVersion: nfd.openshift.io/v1alpha1
kind: NodeFeatureRule
metadata:
  name: {{.OSC_RULES_NAME}}
  namespace: {{.NFD_NAMESPACE}}
spec:
  rules:
    - name: "runtime.kata"
      labels:
        "feature.node.kubernetes.io/runtime.kata": "true"
      matchAny:
        - matchFeatures:
            - feature: cpu.cpuid
              matchExpressions:
                SSE42: {op: Exists}
                VMX: {op: Exists}
            - feature: kernel.loadedmodule
              matchExpressions:
                kvm: {op: Exists}
                kvm_intel: {op: Exists}
        - matchFeatures:
            - feature: cpu.cpuid
              matchExpressions:
                SSE42: {op: Exists}
                SVM: {op: Exists}
            - feature: kernel.loadedmodule
              matchExpressions:
                kvm: {op: Exists}
                kvm_amd: {op: Exists}
`

const intelNfrManifest = `
apiVersion: nfd.openshift.io/v1alpha1
kind: NodeFeatureRule
metadata:
  name: {{.INTEL_NFR_NAME}}
  namespace: {{.INTE_NFR_NAMESPACE}}
spec:
  rules:
    - name: "intel.sgx"
      labels:
        "intel.feature.node.kubernetes.io/sgx": "true"
      extendedResources:
        sgx.intel.com/epc: "@cpu.security.sgx.epc"
      matchFeatures:
        - feature: cpu.cpuid
          matchExpressions:
            SGX: {op: Exists}
            SGXLC: {op: Exists}
        - feature: cpu.security
          matchExpressions:
            sgx.enabled: {op: IsTrue}
        - feature: kernel.config
          matchExpressions:
            X86_SGX: {op: Exists}
    - name: "intel.tdx"
      labels:
        "intel.feature.node.kubernetes.io/tdx": "true"
      extendedResources:
        tdx.intel.com/keys: "@cpu.security.tdx.total_keys"
      matchFeatures:
        - feature: cpu.security
          matchExpressions:
            tdx.enabled: {op: Exists}
`

const amdNfrManifest = `
apiVersion: nfd.openshift.io/v1alpha1
kind: NodeFeatureRule
metadata:
  name: {{.AMD_NFR_NAME}}
  namespace: {{.AMD_NFR_NAMESPACE}}
spec:
  rules:
    - name: "amd.sev-snp"
      labels:
        "amd.feature.node.kubernetes.io/snp": "true"
      extendedResources:
        sev-snp.amd.com/esids: "@cpu.security.sev.encrypted_state_ids"
      matchFeatures:
        - feature: cpu.security
          matchExpressions:
            sev.snp.enabled: { op: Exists }
`

const oscFeaturegateManifest = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{.OSC_FEATURE_GATE_NAME}}
  namespace: {{.OPERATOR_NAMESPACE}}
data:
  confidential: "true"
  deploymentMode: MachineConfig
`
