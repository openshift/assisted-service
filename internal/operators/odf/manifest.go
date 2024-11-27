package odf

import (
	"bytes"
	"text/template"

	"github.com/hashicorp/go-version"
)

type storageInfo struct {
	ODFDisks int64
}

func generateStorageClusterManifest(StorageClusterManifest string, odfDiskCounts int64) ([]byte, error) {
	info := &storageInfo{ODFDisks: odfDiskCounts}
	tmpl, err := template.New("OcsStorageCluster").Parse(StorageClusterManifest)
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, info)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil

}

func Manifests(mode odfDeploymentMode, numberOfDisks int64, openshiftVersion string) (map[string][]byte, []byte, error) {
	openshiftManifests := make(map[string][]byte)
	var odfSC []byte
	var err error

	if mode == compactMode {
		odfSC, err = generateStorageClusterManifest(ocsMinDeploySC, numberOfDisks)
		if err != nil {
			return nil, nil, err
		}
	} else { // use the ODF CR with labelSelector to deploy ODF on only worker nodes
		odfSC, err = generateStorageClusterManifest(ocsSc, numberOfDisks)
		if err != nil {
			return nil, nil, err
		}
	}
	//Check if OCP version is 4.8.x if yes, then return manifests for OCS
	v1, er := version.NewVersion(openshiftVersion)
	if er != nil {
		return nil, nil, er
	}
	constraints, er := version.NewConstraint(">= 4.8, < 4.9")
	if er != nil {
		return nil, nil, er
	}
	if constraints.Check(v1) {
		openshiftManifests["50_openshift-ocs_ns.yaml"] = []byte(odfNamespace)
		ocsSubscription, er := ocsSubscription()
		if er != nil {
			return map[string][]byte{}, []byte{}, er
		}
		openshiftManifests["50_openshift-ocs_subscription.yaml"] = []byte(ocsSubscription)
		openshiftManifests["50_openshift-ocs_operator_group.yaml"] = []byte(odfOperatorGroup)
		return openshiftManifests, odfSC, nil
	}

	//If OCP version is >=4.9 then return manifests for ODF
	openshiftManifests["50_openshift-odf_ns.yaml"] = []byte(odfNamespace)
	odfSubscription, err := odfSubscription()
	if err != nil {
		return map[string][]byte{}, []byte{}, err
	}
	openshiftManifests["50_openshift-odf_subscription.yaml"] = []byte(odfSubscription)
	openshiftManifests["50_openshift-odf_operator_group.yaml"] = []byte(odfOperatorGroup)
	odfSC = append([]byte(odfStorageSystem+"\n---\n"), odfSC...)
	return openshiftManifests, odfSC, nil
}

func ocsSubscription() (string, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         Operator.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME": "ocs-operator",
	}

	const ocsSubscription = `apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: "{{.OPERATOR_SUBSCRIPTION_NAME}}"
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  channel: stable-4.8
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

func odfSubscription() (string, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         Operator.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME": Operator.SubscriptionName,
	}

	const odfSubscription = `apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: "{{.OPERATOR_SUBSCRIPTION_NAME}}"
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  installPlanApproval: Automatic
  name: odf-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace`

	tmpl, err := template.New("ocsSubscription").Parse(odfSubscription)
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

const odfNamespace = `apiVersion: v1
kind: Namespace
metadata:
  labels:
    openshift.io/cluster-monitoring: "true"
  name: openshift-storage
spec: {}`

const odfStorageSystem = `apiVersion: odf.openshift.io/v1alpha1
kind: StorageSystem
metadata:
  name: ocs-storagecluster-storagesystem
  namespace: openshift-storage
spec:
  kind: storagecluster.ocs.openshift.io/v1
  name: ocs-storagecluster
  namespace: openshift-storage`

const odfOperatorGroup = `apiVersion: operators.coreos.com/v1
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
    - count: {{.ODFDisks}}
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

  - count: {{.ODFDisks}}

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
