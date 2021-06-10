package ocs

import (
	"bytes"
	"fmt"
	"text/template"
)

type storageInfo struct {
	OCSDisks int64
}

func getOCSOperatorVersion() string {
	return "4.6"
}

func generateStorageClusterManifest(StorageClusterManifest string, ocsDiskCounts int64) ([]byte, error) {
	info := &storageInfo{OCSDisks: ocsDiskCounts}
	tmpl, err := template.New("OcsStorageCluster").Parse(StorageClusterManifest)
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, info)
	if err != nil {
		return nil, err
	}

	if getOCSOperatorVersion() == "4.6" {
		return buf.Bytes(), nil
	}

	return []byte{}, nil

}

func Manifests(ocsConfig *Config) (map[string][]byte, map[string][]byte, error) {
	openshiftManifests := make(map[string][]byte)
	manifests := make(map[string][]byte)
	var ocsSC []byte
	var err error
	var ocsSubscriptionManifest string
	var ocsCatalogSourceManifest string

	if ocsConfig.OCSDeploymentType == compactMode {
		ocsSC, err = generateStorageClusterManifest(ocsMinDeploySC, ocsConfig.OCSDisksAvailable)
		if err != nil {
			return nil, nil, err
		}
	} else { // use the OCS CR with labelsector to deploy OCS on only worker nodes
		ocsSC, err = generateStorageClusterManifest(ocsSc, ocsConfig.OCSDisksAvailable)
		if err != nil {
			return nil, nil, err
		}
	}
	manifests["99_openshift-ocssc.yaml"] = ocsSC
	openshiftManifests["99_openshift-ocs_ns.yaml"] = []byte(ocsNamespace)

	fmt.Println("TESTING INTERNAL OCS BUILD")
	ocsInternalBuild := ocsConfig.OCSTestInternalBuild
	fmt.Println("OCS BUILD VAL ", ocsInternalBuild)
	if ocsInternalBuild {
		ocsSubscriptionManifest, err = ocsCustomSubscription(ocsConfig.OCSTestSubscriptionChannel)
		if err != nil {
			return map[string][]byte{}, map[string][]byte{}, err
		}
		ocsCatalogSourceManifest, err = ocsCatalogSource(ocsConfig.OCSTestImage)
		if err != nil {
			return map[string][]byte{}, map[string][]byte{}, err
		}
		openshiftManifests["99_openshift-ocs_catalog_source.yaml"] = []byte(ocsCatalogSourceManifest)
	} else {
		ocsSubscriptionManifest, err = ocsSubscription()
		if err != nil {
			return map[string][]byte{}, map[string][]byte{}, err
		}
	}

	openshiftManifests["99_openshift-ocs_subscription.yaml"] = []byte(ocsSubscriptionManifest)
	openshiftManifests["99_openshift-ocs_operator_group.yaml"] = []byte(ocsOperatorGroup)
	return openshiftManifests, manifests, nil
}

func ocsSubscription() (string, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         Operator.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME": Operator.SubscriptionName,
	}

	const ocsSubscription = `apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: "{{.OPERATOR_SUBSCRIPTION_NAME}}"
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  installPlanApproval: Automatic
  name: ocs-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace`

	tmpl, err := template.New("ocsSubscription").Parse(ocsSubscription)
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, data)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

const ocsNamespace = `apiVersion: v1
kind: Namespace
metadata:
  labels:
    openshift.io/cluster-monitoring: "true"
  name: openshift-storage
spec: {}`

const ocsOperatorGroup = `apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: openshift-storage-operatorgroup
  namespace: openshift-storage
spec:
  targetNamespaces:
  - openshift-storage`

const ocsMinDeploySC = `apiVersion: ocs.openshift.io/v1
kind: StorageCluster
metadata:
  name: ocs-storagecluster
  namespace: openshift-storage
spec:
  labelSelector:

    matchExpressions:
      - key: node-role.kubernetes.io/worker
        operator: In
        values:
        - ""
  manageNodes: false
  flexibleScaling: true
  resources:
    mds:
      limits:
        cpu: "3"
        memory: "8Gi"
      requests:
        cpu: "1"
        memory: "8Gi"
    rgw:
      limits:
        cpu: "2"
        memory: "4Gi"
      requests:
        cpu: "1"
        memory: "4Gi"
  monDataDirHostPath: /var/lib/rook
  storageDeviceSets:
    - count: {{.OCSDisks}}
      dataPVCTemplate:
        spec:
          accessModes:
            - ReadWriteOnce
          resources:
            requests:
              storage: "1"
          storageClassName: 'localblock-sc'
          volumeMode: Block
      name: ocs-deviceset
      placement:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: node-role.kubernetes.io/worker
                operator: Exists
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - podAffinityTerm:
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - rook-ceph-osd
              topologyKey: kubernetes.io/hostname
            weight: 100
      preparePlacement:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: node-role.kubernetes.io/worker
                operator: Exists
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - podAffinityTerm:
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - rook-ceph-osd-prepare
              topologyKey: kubernetes.io/hostname
            weight: 100
      portable: false
      replica: 1
      resources:
        limits:
          cpu: "2"
          memory: "5Gi"
        requests:
          cpu: "1"
          memory: "5Gi"`

const ocsSc = `apiVersion: ocs.openshift.io/v1

kind: StorageCluster

metadata:

  name: ocs-storagecluster

  namespace: openshift-storage

spec:

  labelSelector:

    matchExpressions:
      - key: node-role.kubernetes.io/worker
        operator: In
        values:
        - ""

  manageNodes: false
  flexibleScaling: true
  monDataDirHostPath: /var/lib/rook

  storageDeviceSets:

  - count: {{.OCSDisks}}

    dataPVCTemplate:

      spec:

        accessModes:

        - ReadWriteOnce

        resources:

          requests:

            storage: "1"

        storageClassName: 'localblock-sc'

        volumeMode: Block

    name: ocs-deviceset

    placement:
      nodeAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          nodeSelectorTerms:
          - matchExpressions:
            - key: node-role.kubernetes.io/worker
              operator: Exists
      podAntiAffinity:
        preferredDuringSchedulingIgnoredDuringExecution:
        - podAffinityTerm:
            labelSelector:
              matchExpressions:
              - key: app
                operator: In
                values:
                - rook-ceph-osd
            topologyKey: kubernetes.io/hostname
          weight: 100
    preparePlacement:
      nodeAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          nodeSelectorTerms:
          - matchExpressions:
            - key: node-role.kubernetes.io/worker
              operator: Exists
      podAntiAffinity:
        preferredDuringSchedulingIgnoredDuringExecution:
        - podAffinityTerm:
            labelSelector:
              matchExpressions:
              - key: app
                operator: In
                values:
                - rook-ceph-osd-prepare
            topologyKey: kubernetes.io/hostname
          weight: 100

    portable: false

    replica: 1
`
