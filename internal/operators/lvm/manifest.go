package lvm

import (
	"bytes"
	"text/template"

	"github.com/openshift/assisted-service/internal/common"
)

const defaultDeviceName = "vg1"

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

	needsFallbackCatalog, err := common.BaseVersionGreaterOrEqual(LvmsCatalogFallbackMinVersion, cluster.OpenshiftVersion)
	if err != nil {
		return nil, nil, err
	}
	if needsFallbackCatalog {
		lvmCatalogSource, err := getCatalogSource()
		if err != nil {
			return nil, nil, err
		}
		openshiftManifests["50_openshift-lvm_catalog_source.yaml"] = lvmCatalogSource
	}

	openshiftManifests["50_openshift-lvm_ns.yaml"] = lvmNamespace
	openshiftManifests["50_openshift-lvm_operator_group.yaml"] = lvmOperatorGroup
	openshiftManifests["50_openshift-lvm_subscription.yaml"] = lvmSubscription
	return openshiftManifests, lvmcluster, nil
}

func getSubscriptionInfo(openshiftVersion string) (map[string]string, error) {
	isGreaterOrEqual, err := common.BaseVersionGreaterOrEqual(LvmsMinOpenshiftVersion4_12, openshiftVersion)
	if err != nil {
		return map[string]string{}, err
	}

	needsFallbackCatalog, err := common.BaseVersionGreaterOrEqual(LvmsCatalogFallbackMinVersion, openshiftVersion)
	if err != nil {
		return map[string]string{}, err
	}

	catalogSource := "redhat-operators"
	if needsFallbackCatalog {
		catalogSource = LvmsCatalogFallbackName
	}

	if !isGreaterOrEqual {
		return map[string]string{
			"OPERATOR_NAMESPACE":              Operator.Namespace,
			"OPERATOR_SUBSCRIPTION_NAME":      LvmoSubscriptionName,
			"OPERATOR_SUBSCRIPTION_SPEC_NAME": LvmoSubscriptionName,
			"OPERATOR_CATALOG_SOURCE":         catalogSource,
		}, nil
	}

	return map[string]string{
		"OPERATOR_NAMESPACE":              Operator.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME":      LvmsSubscriptionName,
		"OPERATOR_SUBSCRIPTION_SPEC_NAME": LvmsSubscriptionName,
		"OPERATOR_CATALOG_SOURCE":         catalogSource,
	}, nil
}

func getCatalogSource() ([]byte, error) {
	data := map[string]string{
		"CATALOG_SOURCE_NAME":  LvmsCatalogFallbackName,
		"CATALOG_SOURCE_IMAGE": LvmsCatalogFallbackImage,
	}
	return executeTemplate(data, "LvmCatalogSource", LvmCatalogSource)
}

func getSubscription(cluster *common.Cluster) ([]byte, error) {
	subscriptionInfo, err := getSubscriptionInfo(cluster.OpenshiftVersion)
	if err != nil {
		return []byte{}, err
	}

	return executeTemplate(subscriptionInfo, "LvmSubscription", LvmSubscription)
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
		"DEVICE_NAME":        defaultDeviceName,
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
  source: "{{.OPERATOR_CATALOG_SOURCE}}"
  sourceNamespace: openshift-marketplace`

// LvmCatalogSource is injected when the default redhat-operators catalog does not publish
// lvms-operator for the target OCP version. The Subscription is pointed at this source instead.
// TODO: Remove once lvms-operator is published to the redhat-operators catalog for the target version.
const LvmCatalogSource = `apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: "{{.CATALOG_SOURCE_NAME}}"
  namespace: openshift-marketplace
spec:
  displayName: Red Hat Operators v4.21
  image: "{{.CATALOG_SOURCE_IMAGE}}"
  priority: -100
  publisher: Red Hat
  sourceType: grpc
  updateStrategy:
    registryPoll:
      interval: 10m0s`

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
    - name: {{.DEVICE_NAME}}
      thinPoolConfig:
        name: thin-pool-1
        sizePercent: 90
        overprovisionRatio: 10`
