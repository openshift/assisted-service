package lvm

import (
	"bytes"
	"text/template"

	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
)

// Manifests returns manifests needed to deploy LVM
func Manifests(cluster *common.Cluster) (map[string][]byte, []byte, error) {
	lvmSubscription, err := getSubscription(cluster)
	if err != nil {
		return nil, nil, err
	}
	lvmNamespace, err := getNamespace()
	if err != nil {
		return nil, nil, err
	}
	lvmOperatorGroup, err := getOperatorGroup()
	if err != nil {
		return nil, nil, err
	}
	lvmcluster, err := getLvmCluster()
	if err != nil {
		return nil, nil, err
	}

	openshiftManifests := make(map[string][]byte)

	openshiftManifests["50_openshift-lvm_ns.yaml"] = lvmNamespace
	openshiftManifests["50_openshift-lvm_operator_group.yaml"] = lvmOperatorGroup
	openshiftManifests["50_openshift-lvm_subscription.yaml"] = lvmSubscription
	return openshiftManifests, lvmcluster, nil
}

func getSubscription(cluster *common.Cluster) ([]byte, error) {

	ocpVersion, err := version.NewVersion(cluster.OpenshiftVersion)
	if err != nil {
		return []byte{}, err
	}

	minOCPVersionForLVMS, err := version.NewVersion(LvmsMinOpenshiftVersion)
	if err != nil {
		return []byte{}, err
	}

	if ocpVersion.LessThan(minOCPVersionForLVMS) {
		data := map[string]string{
			"OPERATOR_NAMESPACE":              Operator.Namespace,
			"OPERATOR_SUBSCRIPTION_NAME":      lvmoSubscriptionName,
			"OPERATOR_SUBSCRIPTION_SPEC_NAME": lvmoSubscriptionName,
		}
		return executeTemplate(data, "LvmSubscription", LvmSubscription)
	}

	data := map[string]string{
		"OPERATOR_NAMESPACE":              Operator.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME":      lvmsSubscriptionName,
		"OPERATOR_SUBSCRIPTION_SPEC_NAME": lvmsSubscriptionName,
	}

	return executeTemplate(data, "LvmSubscription", LvmSubscription)
}

func getNamespace() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": Operator.Namespace,
	}
	return executeTemplate(data, "LvmNamespace", LvmNamespace)
}

func getOperatorGroup() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": Operator.Namespace,
	}
	return executeTemplate(data, "LvmOperatorGroup", LvmOperatorGroup)
}

func getLvmCluster() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": Operator.Namespace,
	}
	return executeTemplate(data, "LvmCluster", LvmCluster)
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

const LvmSubscription = `apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: "{{.OPERATOR_SUBSCRIPTION_NAME}}"
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  installPlanApproval: Automatic
  name: "{{.OPERATOR_SUBSCRIPTION_SPEC_NAME}}"
  source: redhat-operators
  sourceNamespace: openshift-marketplace`

const LvmNamespace = `apiVersion: v1
kind: Namespace
metadata:
  name: "{{.OPERATOR_NAMESPACE}}"
  labels:
    openshift.io/cluster-monitoring: "true"
spec: {}`

const LvmOperatorGroup = `apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: openshift-storage-operatorgroup
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  targetNamespaces:
  - "{{.OPERATOR_NAMESPACE}}"`

const LvmCluster = `apiVersion: lvm.topolvm.io/v1alpha1
kind: LVMCluster
metadata:
  name: lvmcluster-sample
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  storage:
    deviceClasses:
    - name: vg1
      thinPoolConfig:
        name: thin-pool-1
        sizePercent: 90
        overprovisionRatio: 10`
