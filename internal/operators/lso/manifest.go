package lso

import (
	"bytes"
	"text/template"
)

func lsoSubscription() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         Operator.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME": Operator.SubscriptionName,
	}

	const lsoSubscription = `apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: "{{.OPERATOR_SUBSCRIPTION_NAME}}"
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  installPlanApproval: Automatic
  name: local-storage-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace`

	tmpl, err := template.New("lsoSubscription").Parse(lsoSubscription)
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

func Manifests() (map[string][]byte, []byte, error) {
	lsoSubs, err := lsoSubscription()
	if err != nil {
		return nil, nil, err
	}
	openshiftManifests := make(map[string][]byte)
	openshiftManifests["50_openshift-lso_ns.yaml"] = []byte(localStorageNamespace)
	openshiftManifests["50_openshift-lso_operator_group.yaml"] = []byte(lsoOperatorGroup)
	openshiftManifests["50_openshift-lso_subscription.yaml"] = lsoSubs
	openshiftManifests["50_openshift-lso_prometheus-role.yaml"] = []byte(localStoragePrometheusRole)
	openshiftManifests["50_openshift-lso_prometheus-rolebinding.yaml"] = []byte(localStoragePrometheusRoleBinding)
	return openshiftManifests, []byte(localVolumeSet), nil
}

const lsoOperatorGroup = `apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  annotations:
    olm.providedAPIs: LocalVolume.v1.local.storage.openshift.io
  name: local-storage
  namespace: openshift-local-storage
spec:
  targetNamespaces:
  - openshift-local-storage`

const localStorageNamespace = `apiVersion: v1
kind: Namespace
metadata:
  name: openshift-local-storage
  labels:
    openshift.io/cluster-monitoring: "true"`

const localVolumeSet = `apiVersion: "local.storage.openshift.io/v1alpha1"
kind: "LocalVolumeSet"
metadata:
  name: "local-disks"
  namespace: "openshift-local-storage"
spec:
  storageClassName: "localblock-sc"
  volumeMode: Block
  deviceInclusionSpec:
    deviceTypes:
      - "disk"`

const localStoragePrometheusRole = `apiVersion: "rbac.authorization.k8s.io/v1"
kind: "Role"
metadata:
  name: "openshift-local-storage-prometheus-k8s"
  namespace: "openshift-local-storage"
rules:
- apiGroups:
    - ""
  resources:
    - "services"
    - "endpoints"
    - "pods"
  verbs:
    - "get"
    - "list"
    - "watch"`

const localStoragePrometheusRoleBinding = `apiVersion: "rbac.authorization.k8s.io/v1"
kind: "RoleBinding"
metadata:
  name: "openshift-local-storage-prometheus-k8s"
  namespace: "openshift-local-storage"
roleRef:
  apiGroup: "rbac.authorization.k8s.io"
  kind: "Role"
  name: "openshift-local-storage-prometheus-k8s"
subjects:
  - kind: "ServiceAccount"
    name: "prometheus-k8s"
    namespace: "openshift-monitoring"`
