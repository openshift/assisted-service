package cnv

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
)

const (
	UpstreamNamespace   string = "kubevirt-hyperconverged"
	DownstreamNamespace string = "openshift-cnv"

	upstreamSource   string = "community-operators"
	downstreamSource string = "redhat-operators"

	upstreamSourceName   string = "community-kubevirt-hyperconverged"
	downstreamSourceName string = "kubevirt-hyperconverged"

	minimalOpenShiftVersionForHPPSNO  string = "4.10.0-0.0"
	minimalOpenshiftVersionForLVMOSNO string = "4.12.0-0.0"
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
func Manifests(config Config, cluster *common.Cluster) (map[string][]byte, []byte, error) {
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

	if shouldInstallHPP(config, cluster) {
		cnvHpp, err := hpp(config.SNOPoolSizeRequestHPPGib)
		if err != nil {
			return nil, nil, err
		}
		// Add HostPathProvisioner to CNV manifest
		cnvHco = append(cnvHco, []byte("\n---\n")...)
		cnvHco = append(cnvHco, cnvHpp...)
		openshiftManifests["50_openshift-cnv_hpp_sc.yaml"] = []byte(cnvHPPStorageClass)
	}
	openshiftManifests["50_openshift-cnv_subscription.yaml"] = cnvSubsManifest
	openshiftManifests["50_openshift-cnv_ns.yaml"] = cnvNs
	openshiftManifests["50_openshift-cnv_operator_group.yaml"] = cnvGrp
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

func hpp(diskThresholdGi int64) ([]byte, error) {
	data := map[string]string{
		"STORAGE_SIZE": fmt.Sprintf("%dGi", diskThresholdGi),
	}
	return executeTemplate(data, "cnvHPP", cnvHPPManifestTemplate)
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

func shouldInstallHPP(config Config, cluster *common.Cluster) bool {
	if !common.IsSingleNodeCluster(cluster) || !config.SNOInstallHPP {
		return false
	}

	// HPP on SNO is only supported from CNV 4.10 and OCP < 4.12
	var err error
	var ocpVersion, minimalVersionForHppSno, maximalVersionForHppSno *version.Version
	ocpVersion, err = version.NewVersion(cluster.OpenshiftVersion)
	if err != nil {
		return false
	}
	minimalVersionForHppSno, err = version.NewVersion(minimalOpenShiftVersionForHPPSNO)
	if err != nil {
		return false
	}
	maximalVersionForHppSno, err = version.NewVersion(minimalOpenshiftVersionForLVMOSNO)
	if err != nil {
		return false
	}
	if ocpVersion.LessThan(minimalVersionForHppSno) || ocpVersion.GreaterThanOrEqual(maximalVersionForHppSno) {
		return false
	}

	return true
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

const cnvHPPManifestTemplate = `apiVersion: hostpathprovisioner.kubevirt.io/v1beta1
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
            storage: "{{.STORAGE_SIZE}}"
      path: "/var/hpvolumes"
  workload:
    nodeSelector:
      kubernetes.io/os: linux`

const cnvHPPStorageClass = `apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  annotations:
    storageclass.kubernetes.io/is-default-class: "true"
  name: sno-storage
provisioner: kubevirt.io.hostpath-provisioner
reclaimPolicy: Delete
volumeBindingMode: Immediate
parameters:
  storagePool: sno`
