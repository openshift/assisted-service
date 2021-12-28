package cnv

import (
	"bytes"
	"text/template"
)

const (
	UpstreamNamespace   string = "kubevirt-hyperconverged"
	DownstreamNamespace string = "openshift-cnv"

	upstreamSource   string = "community-operators"
	downstreamSource string = "redhat-operators"

	upstreamSourceName   string = "community-kubevirt-hyperconverged"
	downstreamSourceName string = "kubevirt-hyperconverged"
)

type manifestConfig struct {
	// CNV operator namespace
	Namespace string

	// CNV operator source from where we fetch HCO image
	Source string

	// CNV operator source name used to identify HCO
	SourceName string
}

func configSource(config Config) manifestConfig {
	if config.Mode {
		return manifestConfig{Namespace: DownstreamNamespace, Source: downstreamSource, SourceName: downstreamSourceName}
	}
	return manifestConfig{Namespace: UpstreamNamespace, Source: upstreamSource, SourceName: upstreamSourceName}
}

// Manifests returns manifests needed to deploy CNV
func Manifests(config Config, isSingleNodeCluster bool) (map[string][]byte, []byte, error) {
	configSource := configSource(config)
	cnvSubsManifest, err := subscription(configSource)

	if err != nil {
		return nil, nil, err
	}
	cnvNs, err := namespace(configSource)
	if err != nil {
		return nil, nil, err
	}
	cnvGrp, err := group(configSource)
	if err != nil {
		return nil, nil, err
	}
	cnvHco, err := hco(configSource)
	if err != nil {
		return nil, nil, err
	}

	openshiftManifests := make(map[string][]byte)

	if isSingleNodeCluster {
		openshiftManifests["99_openshift-cnv_hpp.yaml"] = []byte(cnvHPPManifest)
		openshiftManifests["99_openshift-cnv_hpp_sc.yaml"] = []byte(cnvHPPStorageClass)
	}
	openshiftManifests["99_openshift-cnv_subscription.yaml"] = cnvSubsManifest
	openshiftManifests["99_openshift-cnv_ns.yaml"] = cnvNs
	openshiftManifests["99_openshift-cnv_operator_group.yaml"] = cnvGrp
	return openshiftManifests, cnvHco, nil
}

func subscription(config manifestConfig) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         config.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME": Operator.SubscriptionName,
		"OPERATOR_SOURCE":            config.Source,
		"OPERATOR_SOURCE_NAME":       config.SourceName,
	}
	return executeTemplate(data, "cnvSubscription", cnvSubscription)
}

func namespace(config manifestConfig) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": config.Namespace,
	}
	return executeTemplate(data, "cnvNamespace", cnvNamespace)
}

func group(config manifestConfig) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": config.Namespace,
	}
	return executeTemplate(data, "cnvGroup", cnvGroup)
}

func hco(config manifestConfig) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": config.Namespace,
	}
	return executeTemplate(data, "cnvHCO", cnvHCOManifestTemplate)
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

const cnvSubscription = `apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: "{{.OPERATOR_SUBSCRIPTION_NAME}}"
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  source: "{{.OPERATOR_SOURCE}}"
  sourceNamespace: openshift-marketplace
  name: "{{.OPERATOR_SOURCE_NAME}}"
  channel: stable
  installPlanApproval: "Automatic"`

const cnvNamespace = `apiVersion: v1
kind: Namespace
metadata:
  name: "{{.OPERATOR_NAMESPACE}}"`

const cnvGroup = `apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: kubevirt-hyperconverged-group
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  targetNamespaces:
  - "{{.OPERATOR_NAMESPACE}}"`

const cnvHCOManifestTemplate = `apiVersion: hco.kubevirt.io/v1beta1
kind: HyperConverged
metadata:
  name: kubevirt-hyperconverged
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  BareMetalPlatform: true`

const cnvHPPManifest = `apiVersion: hostpathprovisioner.kubevirt.io/v1beta1
kind: HostPathProvisioner
metadata:
  name: hostpath-provisioner
spec:
  imagePullPolicy: IfNotPresent
  storagePools:
    - name: sno
      pvcTemplate:
        storageClassName: localblock-sc
        volumeMode: Block
        accessModes:
        - ReadWriteOnce
        resources:
          requests:
            storage: 50Gi
      path: "/var/hpvolumes"
  workload:
    nodeSelector:
      kubernetes.io/os: linux`

const cnvHPPStorageClass = `apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: sno-storage
provisioner: kubevirt.io.hostpath-provisioner
reclaimPolicy: Delete
volumeBindingMode: Immediate
parameters:
  storagePool: sno`
