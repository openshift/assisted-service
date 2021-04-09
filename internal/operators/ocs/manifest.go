package ocs

import (
	"bytes"
	"text/template"
)

type storageInfo struct {
	OCSDisks int64
}

func getOCSOperatorVersion() string {
	return "4.6"
}

func generateStorageClusterManifest(StorageClusterManifest string, ocsDiskCounts int64) ([]byte, error) {
	// Disk counts are a multiple of 3
	ocsDiskCounts /= 3

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

func Manifests(ocsConfig *Config) (map[string][]byte, error) {
	manifests := make(map[string][]byte)
	var ocsSC []byte
	var err error

	if ocsConfig.OCSDeploymentType == compactMode {
		ocsSC, err = generateStorageClusterManifest(ocsMinDeploySC, ocsConfig.OCSDisksAvailable)
		if err != nil {
			return nil, err
		}
	} else if ocsConfig.OCSDeploymentType == minimalMode { // use separate manifest for minimum deployment of OCS
		ocsSC, err = generateStorageClusterManifest(ocsMinDeploySC, ocsConfig.OCSDisksAvailable)
		if err != nil {
			return nil, err
		}
	} else { // use the OCS CR with labelsector to deploy OCS on only worker nodes
		ocsSC, err = generateStorageClusterManifest(ocsSc, ocsConfig.OCSDisksAvailable)
		if err != nil {
			return nil, err
		}
	}
	manifests["99_openshift-ocssc.yaml"] = ocsSC
	manifests["99_openshift-ocssc_crd.yaml"] = []byte(ocsCRD)
	manifests["99_openshift-ocs_ns.yaml"] = []byte(ocsNamespace)
	ocsSubscription, err := ocsSubscription()
	if err != nil {
		return map[string][]byte{}, err
	}
	manifests["99_openshift-ocs_subscription.yaml"] = []byte(ocsSubscription)
	manifests["99_openshift-ocs_operator_group.yaml"] = []byte(ocsOperatorGroup)
	return manifests, nil
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

const ocsCRD = `apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: storageclusters.ocs.openshift.io
spec:
  additionalPrinterColumns:
  - JSONPath: .metadata.creationTimestamp
    name: Age
    type: date
  - JSONPath: .status.phase
    description: Current Phase
    name: Phase
    type: string
  - JSONPath: .spec.externalStorage.enable
    description: External Storage Cluster
    name: External
    type: boolean
  - JSONPath: .metadata.creationTimestamp
    name: Created At
    type: string
  - JSONPath: .spec.version
    description: Storage Cluster Version
    name: Version
    type: string
  group: ocs.openshift.io
  names:
    kind: StorageCluster
    listKind: StorageClusterList
    plural: storageclusters
    singular: storagecluster
  scope: Namespaced
  subresources:
    status: {}
  validation:
    openAPIV3Schema:
      description: StorageCluster is the Schema for the storageclusters API
      type: object
      properties:
        apiVersion:
          description: 'APIVersion defines the versioned schema of this representation
            of an object. Servers should convert recognized schemas to the latest
            internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
          type: string
        kind:
          description: 'Kind is a string value representing the REST resource this
            object represents. Servers may infer this from the endpoint the client
            submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
          type: string
        metadata:
          type: object
        spec:
          description: StorageClusterSpec defines the desired state of StorageCluster
          type: object
          properties:
            encryption:
              description: EncryptionSpec defines if encryption should be enabled
                for the Storage Cluster It is optional and defaults to false.
              type: object
              properties:
                enable:
                  type: boolean
            externalStorage:
              description: External Storage is optional and defaults to false. When
                set to true, OCS will connect to an external OCS Storage Cluster instead
                of provisioning one locally.
              type: object
              properties:
                enable:
                  type: boolean
            flexibleScaling:
              description: If enabled, sets the failureDomain to host, allowing devices
                to be distributed evenly across all nodes, regardless of distribution
                in zones or racks.
              type: boolean
            hostNetwork:
              description: HostNetwork defaults to false
              type: boolean
            instanceType:
              type: string
            labelSelector:
              description: LabelSelector is used to specify custom labels of nodes
                to run OCS on
              type: object
              properties:
                matchExpressions:
                  description: matchExpressions is a list of label selector requirements.
                    The requirements are ANDed.
                  type: array
                  items:
                    description: A label selector requirement is a selector that contains
                      values, a key, and an operator that relates the key and values.
                    type: object
                    required:
                    - key
                    - operator
                    properties:
                      key:
                        description: key is the label key that the selector applies
                          to.
                        type: string
                      operator:
                        description: operator represents a key's relationship to a
                          set of values. Valid operators are In, NotIn, Exists and
                          DoesNotExist.
                        type: string
                      values:
                        description: values is an array of string values. If the operator
                          is In or NotIn, the values array must be non-empty. If the
                          operator is Exists or DoesNotExist, the values array must
                          be empty. This array is replaced during a strategic merge
                          patch.
                        type: array
                        items:
                          type: string
                matchLabels:
                  description: matchLabels is a map of {key,value} pairs. A single
                    {key,value} in the matchLabels map is equivalent to an element
                    of matchExpressions, whose key field is "key", the operator is
                    "In", and the values array contains only "value". The requirements
                    are ANDed.
                  type: object
                  additionalProperties:
                    type: string
            manageNodes:
              type: boolean
            managedResources:
              description: ManagedResources specifies how to deal with auxiliary resources
                reconciled with the StorageCluster
              type: object
              properties:
                cephBlockPools:
                  description: ManageCephBlockPools defines how to reconcilea CephBlockPools
                  type: object
                  properties:
                    reconcileStrategy:
                      type: string
                cephFilesystems:
                  description: ManageCephFilesystems defines how to reconcile CephFilesystems
                  type: object
                  properties:
                    reconcileStrategy:
                      type: string
                cephObjectStoreUsers:
                  description: ManageCephObjectStoreUsers defines how to reconcile
                    CephObjectStoreUsers
                  type: object
                  properties:
                    reconcileStrategy:
                      type: string
                cephObjectStores:
                  description: ManageCephObjectStores defines how to reconcile CephObjectStores
                  type: object
                  properties:
                    reconcileStrategy:
                      type: string
                snapshotClasses:
                  description: ManageSnapshotClasses defines how to reconcile SnapshotClasses
                  type: object
                  properties:
                    reconcileStrategy:
                      type: string
                storageClasses:
                  description: ManageStorageClasses defines how to reconcile StorageClasses
                  type: object
                  properties:
                    reconcileStrategy:
                      type: string
            monDataDirHostPath:
              type: string
            monPVCTemplate:
              description: PersistentVolumeClaim is a user's request for and claim
                to a persistent volume
              type: object
              properties:
                apiVersion:
                  description: 'APIVersion defines the versioned schema of this representation
                    of an object. Servers should convert recognized schemas to the
                    latest internal value, and may reject unrecognized values. More
                    info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
                  type: string
                kind:
                  description: 'Kind is a string value representing the REST resource
                    this object represents. Servers may infer this from the endpoint
                    the client submits requests to. Cannot be updated. In CamelCase.
                    More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
                  type: string
                metadata:
                  description: 'Standard object''s metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata'
                  type: object
                spec:
                  description: 'Spec defines the desired characteristics of a volume
                    requested by a pod author. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims'
                  type: object
                  properties:
                    accessModes:
                      description: 'AccessModes contains the desired access modes
                        the volume should have. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#access-modes-1'
                      type: array
                      items:
                        type: string
                    dataSource:
                      description: This field requires the VolumeSnapshotDataSource
                        alpha feature gate to be enabled and currently VolumeSnapshot
                        is the only supported data source. If the provisioner can
                        support VolumeSnapshot data source, it will create a new volume
                        and data will be restored to the volume at the same time.
                        If the provisioner does not support VolumeSnapshot data source,
                        volume will not be created and the failure will be reported
                        as an event. In the future, we plan to support more data source
                        types and the behavior of the provisioner may change.
                      type: object
                      required:
                      - kind
                      - name
                      properties:
                        apiGroup:
                          description: APIGroup is the group for the resource being
                            referenced. If APIGroup is not specified, the specified
                            Kind must be in the core API group. For any other third-party
                            types, APIGroup is required.
                          type: string
                        kind:
                          description: Kind is the type of resource being referenced
                          type: string
                        name:
                          description: Name is the name of resource being referenced
                          type: string
                    resources:
                      description: 'Resources represents the minimum resources the
                        volume should have. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#resources'
                      type: object
                      properties:
                        limits:
                          description: 'Limits describes the maximum amount of compute
                            resources allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                          type: object
                          additionalProperties:
                            pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                            anyOf:
                            - type: integer
                            - type: string
                            x-kubernetes-int-or-string: true
                        requests:
                          description: 'Requests describes the minimum amount of compute
                            resources required. If Requests is omitted for a container,
                            it defaults to Limits if that is explicitly specified,
                            otherwise to an implementation-defined value. More info:
                            https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                          type: object
                          additionalProperties:
                            pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                            anyOf:
                            - type: integer
                            - type: string
                            x-kubernetes-int-or-string: true
                    selector:
                      description: A label query over volumes to consider for binding.
                      type: object
                      properties:
                        matchExpressions:
                          description: matchExpressions is a list of label selector
                            requirements. The requirements are ANDed.
                          type: array
                          items:
                            description: A label selector requirement is a selector
                              that contains values, a key, and an operator that relates
                              the key and values.
                            type: object
                            required:
                            - key
                            - operator
                            properties:
                              key:
                                description: key is the label key that the selector
                                  applies to.
                                type: string
                              operator:
                                description: operator represents a key's relationship
                                  to a set of values. Valid operators are In, NotIn,
                                  Exists and DoesNotExist.
                                type: string
                              values:
                                description: values is an array of string values.
                                  If the operator is In or NotIn, the values array
                                  must be non-empty. If the operator is Exists or
                                  DoesNotExist, the values array must be empty. This
                                  array is replaced during a strategic merge patch.
                                type: array
                                items:
                                  type: string
                        matchLabels:
                          description: matchLabels is a map of {key,value} pairs.
                            A single {key,value} in the matchLabels map is equivalent
                            to an element of matchExpressions, whose key field is
                            "key", the operator is "In", and the values array contains
                            only "value". The requirements are ANDed.
                          type: object
                          additionalProperties:
                            type: string
                    storageClassName:
                      description: 'Name of the StorageClass required by the claim.
                        More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#class-1'
                      type: string
                    volumeMode:
                      description: volumeMode defines what type of volume is required
                        by the claim. Value of Filesystem is implied when not included
                        in claim spec. This is a beta feature.
                      type: string
                    volumeName:
                      description: VolumeName is the binding reference to the PersistentVolume
                        backing this claim.
                      type: string
                status:
                  description: 'Status represents the current information/status of
                    a persistent volume claim. Read-only. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims'
                  type: object
                  properties:
                    accessModes:
                      description: 'AccessModes contains the actual access modes the
                        volume backing the PVC has. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#access-modes-1'
                      type: array
                      items:
                        type: string
                    capacity:
                      description: Represents the actual resources of the underlying
                        volume.
                      type: object
                      additionalProperties:
                        pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                        anyOf:
                        - type: integer
                        - type: string
                        x-kubernetes-int-or-string: true
                    conditions:
                      description: Current Condition of persistent volume claim. If
                        underlying persistent volume is being resized then the Condition
                        will be set to 'ResizeStarted'.
                      type: array
                      items:
                        description: PersistentVolumeClaimCondition contails details
                          about state of pvc
                        type: object
                        required:
                        - status
                        - type
                        properties:
                          lastProbeTime:
                            description: Last time we probed the condition.
                            type: string
                            format: date-time
                          lastTransitionTime:
                            description: Last time the condition transitioned from
                              one status to another.
                            type: string
                            format: date-time
                          message:
                            description: Human-readable message indicating details
                              about last transition.
                            type: string
                          reason:
                            description: Unique, this should be a short, machine understandable
                              string that gives the reason for condition's last transition.
                              If it reports "ResizeStarted" that means the underlying
                              persistent volume is being resized.
                            type: string
                          status:
                            type: string
                          type:
                            description: PersistentVolumeClaimConditionType is a valid
                              value of PersistentVolumeClaimCondition.Type
                            type: string
                    phase:
                      description: Phase represents the current phase of PersistentVolumeClaim.
                      type: string
            multiCloudGateway:
              description: MultiCloudGatewaySpec defines specific multi-cloud gateway
                configuration options
              type: object
              properties:
                endpoints:
                  description: Endpoints (optional) sets configuration info for the
                    noobaa endpoint deployment.
                  type: object
                  properties:
                    additionalVirtualHosts:
                      description: 'AdditionalVirtualHosts (optional) provide a list
                        of additional hostnames (on top of the buildin names defined
                        by the cluster: service name, elb name, route name) to be
                        used as virtual hosts by the the endpoints in the endpoint
                        deployment'
                      type: array
                      items:
                        type: string
                    maxCount:
                      description: MaxCount, the number of endpoint instances (pods)
                        to be used as the upper bound when autoscaling
                      type: integer
                      format: int32
                    minCount:
                      description: MinCount, the number of endpoint instances (pods)
                        to be used as the lower bound when autoscaling
                      type: integer
                      format: int32
                    resources:
                      description: Resources (optional) overrides the default resource
                        requirements for every endpoint pod
                      type: object
                      properties:
                        limits:
                          description: 'Limits describes the maximum amount of compute
                            resources allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                          type: object
                          additionalProperties:
                            pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                            anyOf:
                            - type: integer
                            - type: string
                            x-kubernetes-int-or-string: true
                        requests:
                          description: 'Requests describes the minimum amount of compute
                            resources required. If Requests is omitted for a container,
                            it defaults to Limits if that is explicitly specified,
                            otherwise to an implementation-defined value. More info:
                            https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                          type: object
                          additionalProperties:
                            pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                            anyOf:
                            - type: integer
                            - type: string
                            x-kubernetes-int-or-string: true
                reconcileStrategy:
                  description: ReconcileStrategy specifies whether to reconcile NooBaa
                    CRs. Valid values are "manage", "standalone", "ignore" (same as
                    "standalone"), and "" (same as "manage").
                  type: string
            network:
              description: Network represents cluster network settings
              type: object
              required:
              - provider
              - selectors
              properties:
                provider:
                  description: Provider is what provides network connectivity to the
                    cluster e.g. "host" or "multus"
                  type: string
                selectors:
                  description: Selectors string values describe what networks will
                    be used to connect the cluster. Meanwhile the keys describe each
                    network respective responsibilities or any metadata storage provider
                    decide.
                  type: object
                  additionalProperties:
                    type: string
            placement:
              description: Placement is optional and used to specify placements of
                OCS components explicitly
              type: object
              additionalProperties:
                type: object
                properties:
                  nodeAffinity:
                    description: Node affinity is a group of node affinity scheduling
                      rules.
                    type: object
                    properties:
                      preferredDuringSchedulingIgnoredDuringExecution:
                        description: The scheduler will prefer to schedule pods to
                          nodes that satisfy the affinity expressions specified by
                          this field, but it may choose a node that violates one or
                          more of the expressions. The node that is most preferred
                          is the one with the greatest sum of weights, i.e. for each
                          node that meets all of the scheduling requirements (resource
                          request, requiredDuringScheduling affinity expressions,
                          etc.), compute a sum by iterating through the elements of
                          this field and adding "weight" to the sum if the node matches
                          the corresponding matchExpressions; the node(s) with the
                          highest sum are the most preferred.
                        type: array
                        items:
                          description: An empty preferred scheduling term matches
                            all objects with implicit weight 0 (i.e. it's a no-op).
                            A null preferred scheduling term matches no objects (i.e.
                            is also a no-op).
                          type: object
                          required:
                          - preference
                          - weight
                          properties:
                            preference:
                              description: A node selector term, associated with the
                                corresponding weight.
                              type: object
                              properties:
                                matchExpressions:
                                  description: A list of node selector requirements
                                    by node's labels.
                                  type: array
                                  items:
                                    description: A node selector requirement is a
                                      selector that contains values, a key, and an
                                      operator that relates the key and values.
                                    type: object
                                    required:
                                    - key
                                    - operator
                                    properties:
                                      key:
                                        description: The label key that the selector
                                          applies to.
                                        type: string
                                      operator:
                                        description: Represents a key's relationship
                                          to a set of values. Valid operators are
                                          In, NotIn, Exists, DoesNotExist. Gt, and
                                          Lt.
                                        type: string
                                      values:
                                        description: An array of string values. If
                                          the operator is In or NotIn, the values
                                          array must be non-empty. If the operator
                                          is Exists or DoesNotExist, the values array
                                          must be empty. If the operator is Gt or
                                          Lt, the values array must have a single
                                          element, which will be interpreted as an
                                          integer. This array is replaced during a
                                          strategic merge patch.
                                        type: array
                                        items:
                                          type: string
                                matchFields:
                                  description: A list of node selector requirements
                                    by node's fields.
                                  type: array
                                  items:
                                    description: A node selector requirement is a
                                      selector that contains values, a key, and an
                                      operator that relates the key and values.
                                    type: object
                                    required:
                                    - key
                                    - operator
                                    properties:
                                      key:
                                        description: The label key that the selector
                                          applies to.
                                        type: string
                                      operator:
                                        description: Represents a key's relationship
                                          to a set of values. Valid operators are
                                          In, NotIn, Exists, DoesNotExist. Gt, and
                                          Lt.
                                        type: string
                                      values:
                                        description: An array of string values. If
                                          the operator is In or NotIn, the values
                                          array must be non-empty. If the operator
                                          is Exists or DoesNotExist, the values array
                                          must be empty. If the operator is Gt or
                                          Lt, the values array must have a single
                                          element, which will be interpreted as an
                                          integer. This array is replaced during a
                                          strategic merge patch.
                                        type: array
                                        items:
                                          type: string
                            weight:
                              description: Weight associated with matching the corresponding
                                nodeSelectorTerm, in the range 1-100.
                              type: integer
                              format: int32
                      requiredDuringSchedulingIgnoredDuringExecution:
                        description: If the affinity requirements specified by this
                          field are not met at scheduling time, the pod will not be
                          scheduled onto the node. If the affinity requirements specified
                          by this field cease to be met at some point during pod execution
                          (e.g. due to an update), the system may or may not try to
                          eventually evict the pod from its node.
                        type: object
                        required:
                        - nodeSelectorTerms
                        properties:
                          nodeSelectorTerms:
                            description: Required. A list of node selector terms.
                              The terms are ORed.
                            type: array
                            items:
                              description: A null or empty node selector term matches
                                no objects. The requirements of them are ANDed. The
                                TopologySelectorTerm type implements a subset of the
                                NodeSelectorTerm.
                              type: object
                              properties:
                                matchExpressions:
                                  description: A list of node selector requirements
                                    by node's labels.
                                  type: array
                                  items:
                                    description: A node selector requirement is a
                                      selector that contains values, a key, and an
                                      operator that relates the key and values.
                                    type: object
                                    required:
                                    - key
                                    - operator
                                    properties:
                                      key:
                                        description: The label key that the selector
                                          applies to.
                                        type: string
                                      operator:
                                        description: Represents a key's relationship
                                          to a set of values. Valid operators are
                                          In, NotIn, Exists, DoesNotExist. Gt, and
                                          Lt.
                                        type: string
                                      values:
                                        description: An array of string values. If
                                          the operator is In or NotIn, the values
                                          array must be non-empty. If the operator
                                          is Exists or DoesNotExist, the values array
                                          must be empty. If the operator is Gt or
                                          Lt, the values array must have a single
                                          element, which will be interpreted as an
                                          integer. This array is replaced during a
                                          strategic merge patch.
                                        type: array
                                        items:
                                          type: string
                                matchFields:
                                  description: A list of node selector requirements
                                    by node's fields.
                                  type: array
                                  items:
                                    description: A node selector requirement is a
                                      selector that contains values, a key, and an
                                      operator that relates the key and values.
                                    type: object
                                    required:
                                    - key
                                    - operator
                                    properties:
                                      key:
                                        description: The label key that the selector
                                          applies to.
                                        type: string
                                      operator:
                                        description: Represents a key's relationship
                                          to a set of values. Valid operators are
                                          In, NotIn, Exists, DoesNotExist. Gt, and
                                          Lt.
                                        type: string
                                      values:
                                        description: An array of string values. If
                                          the operator is In or NotIn, the values
                                          array must be non-empty. If the operator
                                          is Exists or DoesNotExist, the values array
                                          must be empty. If the operator is Gt or
                                          Lt, the values array must have a single
                                          element, which will be interpreted as an
                                          integer. This array is replaced during a
                                          strategic merge patch.
                                        type: array
                                        items:
                                          type: string
                  podAffinity:
                    description: Pod affinity is a group of inter pod affinity scheduling
                      rules.
                    type: object
                    properties:
                      preferredDuringSchedulingIgnoredDuringExecution:
                        description: The scheduler will prefer to schedule pods to
                          nodes that satisfy the affinity expressions specified by
                          this field, but it may choose a node that violates one or
                          more of the expressions. The node that is most preferred
                          is the one with the greatest sum of weights, i.e. for each
                          node that meets all of the scheduling requirements (resource
                          request, requiredDuringScheduling affinity expressions,
                          etc.), compute a sum by iterating through the elements of
                          this field and adding "weight" to the sum if the node has
                          pods which matches the corresponding podAffinityTerm; the
                          node(s) with the highest sum are the most preferred.
                        type: array
                        items:
                          description: The weights of all of the matched WeightedPodAffinityTerm
                            fields are added per-node to find the most preferred node(s)
                          type: object
                          required:
                          - podAffinityTerm
                          - weight
                          properties:
                            podAffinityTerm:
                              description: Required. A pod affinity term, associated
                                with the corresponding weight.
                              type: object
                              required:
                              - topologyKey
                              properties:
                                labelSelector:
                                  description: A label query over a set of resources,
                                    in this case pods.
                                  type: object
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      type: array
                                      items:
                                        description: A label selector requirement
                                          is a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: operator represents a key's
                                              relationship to a set of values. Valid
                                              operators are In, NotIn, Exists and
                                              DoesNotExist.
                                            type: string
                                          values:
                                            description: values is an array of string
                                              values. If the operator is In or NotIn,
                                              the values array must be non-empty.
                                              If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This
                                              array is replaced during a strategic
                                              merge patch.
                                            type: array
                                            items:
                                              type: string
                                    matchLabels:
                                      description: matchLabels is a map of {key,value}
                                        pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions,
                                        whose key field is "key", the operator is
                                        "In", and the values array contains only "value".
                                        The requirements are ANDed.
                                      type: object
                                      additionalProperties:
                                        type: string
                                namespaces:
                                  description: namespaces specifies which namespaces
                                    the labelSelector applies to (matches against);
                                    null or empty list means "this pod's namespace"
                                  type: array
                                  items:
                                    type: string
                                topologyKey:
                                  description: This pod should be co-located (affinity)
                                    or not co-located (anti-affinity) with the pods
                                    matching the labelSelector in the specified namespaces,
                                    where co-located is defined as running on a node
                                    whose value of the label with key topologyKey
                                    matches that of any node on which any of the selected
                                    pods is running. Empty topologyKey is not allowed.
                                  type: string
                            weight:
                              description: weight associated with matching the corresponding
                                podAffinityTerm, in the range 1-100.
                              type: integer
                              format: int32
                      requiredDuringSchedulingIgnoredDuringExecution:
                        description: If the affinity requirements specified by this
                          field are not met at scheduling time, the pod will not be
                          scheduled onto the node. If the affinity requirements specified
                          by this field cease to be met at some point during pod execution
                          (e.g. due to a pod label update), the system may or may
                          not try to eventually evict the pod from its node. When
                          there are multiple elements, the lists of nodes corresponding
                          to each podAffinityTerm are intersected, i.e. all terms
                          must be satisfied.
                        type: array
                        items:
                          description: Defines a set of pods (namely those matching
                            the labelSelector relative to the given namespace(s))
                            that this pod should be co-located (affinity) or not co-located
                            (anti-affinity) with, where co-located is defined as running
                            on a node whose value of the label with key <topologyKey>
                            matches that of any node on which a pod of the set of
                            pods is running
                          type: object
                          required:
                          - topologyKey
                          properties:
                            labelSelector:
                              description: A label query over a set of resources,
                                in this case pods.
                              type: object
                              properties:
                                matchExpressions:
                                  description: matchExpressions is a list of label
                                    selector requirements. The requirements are ANDed.
                                  type: array
                                  items:
                                    description: A label selector requirement is a
                                      selector that contains values, a key, and an
                                      operator that relates the key and values.
                                    type: object
                                    required:
                                    - key
                                    - operator
                                    properties:
                                      key:
                                        description: key is the label key that the
                                          selector applies to.
                                        type: string
                                      operator:
                                        description: operator represents a key's relationship
                                          to a set of values. Valid operators are
                                          In, NotIn, Exists and DoesNotExist.
                                        type: string
                                      values:
                                        description: values is an array of string
                                          values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the
                                          operator is Exists or DoesNotExist, the
                                          values array must be empty. This array is
                                          replaced during a strategic merge patch.
                                        type: array
                                        items:
                                          type: string
                                matchLabels:
                                  description: matchLabels is a map of {key,value}
                                    pairs. A single {key,value} in the matchLabels
                                    map is equivalent to an element of matchExpressions,
                                    whose key field is "key", the operator is "In",
                                    and the values array contains only "value". The
                                    requirements are ANDed.
                                  type: object
                                  additionalProperties:
                                    type: string
                            namespaces:
                              description: namespaces specifies which namespaces the
                                labelSelector applies to (matches against); null or
                                empty list means "this pod's namespace"
                              type: array
                              items:
                                type: string
                            topologyKey:
                              description: This pod should be co-located (affinity)
                                or not co-located (anti-affinity) with the pods matching
                                the labelSelector in the specified namespaces, where
                                co-located is defined as running on a node whose value
                                of the label with key topologyKey matches that of
                                any node on which any of the selected pods is running.
                                Empty topologyKey is not allowed.
                              type: string
                  podAntiAffinity:
                    description: Pod anti affinity is a group of inter pod anti affinity
                      scheduling rules.
                    type: object
                    properties:
                      preferredDuringSchedulingIgnoredDuringExecution:
                        description: The scheduler will prefer to schedule pods to
                          nodes that satisfy the anti-affinity expressions specified
                          by this field, but it may choose a node that violates one
                          or more of the expressions. The node that is most preferred
                          is the one with the greatest sum of weights, i.e. for each
                          node that meets all of the scheduling requirements (resource
                          request, requiredDuringScheduling anti-affinity expressions,
                          etc.), compute a sum by iterating through the elements of
                          this field and adding "weight" to the sum if the node has
                          pods which matches the corresponding podAffinityTerm; the
                          node(s) with the highest sum are the most preferred.
                        type: array
                        items:
                          description: The weights of all of the matched WeightedPodAffinityTerm
                            fields are added per-node to find the most preferred node(s)
                          type: object
                          required:
                          - podAffinityTerm
                          - weight
                          properties:
                            podAffinityTerm:
                              description: Required. A pod affinity term, associated
                                with the corresponding weight.
                              type: object
                              required:
                              - topologyKey
                              properties:
                                labelSelector:
                                  description: A label query over a set of resources,
                                    in this case pods.
                                  type: object
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      type: array
                                      items:
                                        description: A label selector requirement
                                          is a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: operator represents a key's
                                              relationship to a set of values. Valid
                                              operators are In, NotIn, Exists and
                                              DoesNotExist.
                                            type: string
                                          values:
                                            description: values is an array of string
                                              values. If the operator is In or NotIn,
                                              the values array must be non-empty.
                                              If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This
                                              array is replaced during a strategic
                                              merge patch.
                                            type: array
                                            items:
                                              type: string
                                    matchLabels:
                                      description: matchLabels is a map of {key,value}
                                        pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions,
                                        whose key field is "key", the operator is
                                        "In", and the values array contains only "value".
                                        The requirements are ANDed.
                                      type: object
                                      additionalProperties:
                                        type: string
                                namespaces:
                                  description: namespaces specifies which namespaces
                                    the labelSelector applies to (matches against);
                                    null or empty list means "this pod's namespace"
                                  type: array
                                  items:
                                    type: string
                                topologyKey:
                                  description: This pod should be co-located (affinity)
                                    or not co-located (anti-affinity) with the pods
                                    matching the labelSelector in the specified namespaces,
                                    where co-located is defined as running on a node
                                    whose value of the label with key topologyKey
                                    matches that of any node on which any of the selected
                                    pods is running. Empty topologyKey is not allowed.
                                  type: string
                            weight:
                              description: weight associated with matching the corresponding
                                podAffinityTerm, in the range 1-100.
                              type: integer
                              format: int32
                      requiredDuringSchedulingIgnoredDuringExecution:
                        description: If the anti-affinity requirements specified by
                          this field are not met at scheduling time, the pod will
                          not be scheduled onto the node. If the anti-affinity requirements
                          specified by this field cease to be met at some point during
                          pod execution (e.g. due to a pod label update), the system
                          may or may not try to eventually evict the pod from its
                          node. When there are multiple elements, the lists of nodes
                          corresponding to each podAffinityTerm are intersected, i.e.
                          all terms must be satisfied.
                        type: array
                        items:
                          description: Defines a set of pods (namely those matching
                            the labelSelector relative to the given namespace(s))
                            that this pod should be co-located (affinity) or not co-located
                            (anti-affinity) with, where co-located is defined as running
                            on a node whose value of the label with key <topologyKey>
                            matches that of any node on which a pod of the set of
                            pods is running
                          type: object
                          required:
                          - topologyKey
                          properties:
                            labelSelector:
                              description: A label query over a set of resources,
                                in this case pods.
                              type: object
                              properties:
                                matchExpressions:
                                  description: matchExpressions is a list of label
                                    selector requirements. The requirements are ANDed.
                                  type: array
                                  items:
                                    description: A label selector requirement is a
                                      selector that contains values, a key, and an
                                      operator that relates the key and values.
                                    type: object
                                    required:
                                    - key
                                    - operator
                                    properties:
                                      key:
                                        description: key is the label key that the
                                          selector applies to.
                                        type: string
                                      operator:
                                        description: operator represents a key's relationship
                                          to a set of values. Valid operators are
                                          In, NotIn, Exists and DoesNotExist.
                                        type: string
                                      values:
                                        description: values is an array of string
                                          values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the
                                          operator is Exists or DoesNotExist, the
                                          values array must be empty. This array is
                                          replaced during a strategic merge patch.
                                        type: array
                                        items:
                                          type: string
                                matchLabels:
                                  description: matchLabels is a map of {key,value}
                                    pairs. A single {key,value} in the matchLabels
                                    map is equivalent to an element of matchExpressions,
                                    whose key field is "key", the operator is "In",
                                    and the values array contains only "value". The
                                    requirements are ANDed.
                                  type: object
                                  additionalProperties:
                                    type: string
                            namespaces:
                              description: namespaces specifies which namespaces the
                                labelSelector applies to (matches against); null or
                                empty list means "this pod's namespace"
                              type: array
                              items:
                                type: string
                            topologyKey:
                              description: This pod should be co-located (affinity)
                                or not co-located (anti-affinity) with the pods matching
                                the labelSelector in the specified namespaces, where
                                co-located is defined as running on a node whose value
                                of the label with key topologyKey matches that of
                                any node on which any of the selected pods is running.
                                Empty topologyKey is not allowed.
                              type: string
                  tolerations:
                    type: array
                    items:
                      description: The pod this Toleration is attached to tolerates
                        any taint that matches the triple <key,value,effect> using
                        the matching operator <operator>.
                      type: object
                      properties:
                        effect:
                          description: Effect indicates the taint effect to match.
                            Empty means match all taint effects. When specified, allowed
                            values are NoSchedule, PreferNoSchedule and NoExecute.
                          type: string
                        key:
                          description: Key is the taint key that the toleration applies
                            to. Empty means match all taint keys. If the key is empty,
                            operator must be Exists; this combination means to match
                            all values and all keys.
                          type: string
                        operator:
                          description: Operator represents a key's relationship to
                            the value. Valid operators are Exists and Equal. Defaults
                            to Equal. Exists is equivalent to wildcard for value,
                            so that a pod can tolerate all taints of a particular
                            category.
                          type: string
                        tolerationSeconds:
                          description: TolerationSeconds represents the period of
                            time the toleration (which must be of effect NoExecute,
                            otherwise this field is ignored) tolerates the taint.
                            By default, it is not set, which means tolerate the taint
                            forever (do not evict). Zero and negative values will
                            be treated as 0 (evict immediately) by the system.
                          type: integer
                          format: int64
                        value:
                          description: Value is the taint value the toleration matches
                            to. If the operator is Exists, the value should be empty,
                            otherwise just a regular string.
                          type: string
                  topologySpreadConstraints:
                    type: array
                    items:
                      description: TopologySpreadConstraint specifies how to spread
                        matching pods among the given topology.
                      type: object
                      required:
                      - maxSkew
                      - topologyKey
                      - whenUnsatisfiable
                      properties:
                        labelSelector:
                          description: LabelSelector is used to find matching pods.
                            Pods that match this label selector are counted to determine
                            the number of pods in their corresponding topology domain.
                          type: object
                          properties:
                            matchExpressions:
                              description: matchExpressions is a list of label selector
                                requirements. The requirements are ANDed.
                              type: array
                              items:
                                description: A label selector requirement is a selector
                                  that contains values, a key, and an operator that
                                  relates the key and values.
                                type: object
                                required:
                                - key
                                - operator
                                properties:
                                  key:
                                    description: key is the label key that the selector
                                      applies to.
                                    type: string
                                  operator:
                                    description: operator represents a key's relationship
                                      to a set of values. Valid operators are In,
                                      NotIn, Exists and DoesNotExist.
                                    type: string
                                  values:
                                    description: values is an array of string values.
                                      If the operator is In or NotIn, the values array
                                      must be non-empty. If the operator is Exists
                                      or DoesNotExist, the values array must be empty.
                                      This array is replaced during a strategic merge
                                      patch.
                                    type: array
                                    items:
                                      type: string
                            matchLabels:
                              description: matchLabels is a map of {key,value} pairs.
                                A single {key,value} in the matchLabels map is equivalent
                                to an element of matchExpressions, whose key field
                                is "key", the operator is "In", and the values array
                                contains only "value". The requirements are ANDed.
                              type: object
                              additionalProperties:
                                type: string
                        maxSkew:
                          description: 'MaxSkew describes the degree to which pods
                            may be unevenly distributed. It''s the maximum permitted
                            difference between the number of matching pods in any
                            two topology domains of a given topology type. For example,
                            in a 3-zone cluster, MaxSkew is set to 1, and pods with
                            the same labelSelector spread as 1/1/0: | zone1 | zone2
                            | zone3 | |   P   |   P   |       | - if MaxSkew is 1,
                            incoming pod can only be scheduled to zone3 to become
                            1/1/1; scheduling it onto zone1(zone2) would make the
                            ActualSkew(2-0) on zone1(zone2) violate MaxSkew(1). -
                            if MaxSkew is 2, incoming pod can be scheduled onto any
                            zone. It''s a required field. Default value is 1 and 0
                            is not allowed.'
                          type: integer
                          format: int32
                        topologyKey:
                          description: TopologyKey is the key of node labels. Nodes
                            that have a label with this key and identical values are
                            considered to be in the same topology. We consider each
                            <key, value> as a "bucket", and try to put balanced number
                            of pods into each bucket. It's a required field.
                          type: string
                        whenUnsatisfiable:
                          description: 'WhenUnsatisfiable indicates how to deal with
                            a pod if it doesn''t satisfy the spread constraint. -
                            DoNotSchedule (default) tells the scheduler not to schedule
                            it - ScheduleAnyway tells the scheduler to still schedule
                            it It''s considered as "Unsatisfiable" if and only if
                            placing incoming pod on any topology violates "MaxSkew".
                            For example, in a 3-zone cluster, MaxSkew is set to 1,
                            and pods with the same labelSelector spread as 3/1/1:
                            | zone1 | zone2 | zone3 | | P P P |   P   |   P   | If
                            WhenUnsatisfiable is set to DoNotSchedule, incoming pod
                            can only be scheduled to zone2(zone3) to become 3/2/1(3/1/2)
                            as ActualSkew(2-1) on zone2(zone3) satisfies MaxSkew(1).
                            In other words, the cluster can still be imbalanced, but
                            scheduler won''t make it *more* imbalanced. It''s a required
                            field.'
                          type: string
            resources:
              description: Resources follows the conventions of and is mapped to CephCluster.Spec.Resources
              type: object
              additionalProperties:
                description: ResourceRequirements describes the compute resource requirements.
                type: object
                properties:
                  limits:
                    description: 'Limits describes the maximum amount of compute resources
                      allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                    type: object
                    additionalProperties:
                      pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                      anyOf:
                      - type: integer
                      - type: string
                      x-kubernetes-int-or-string: true
                  requests:
                    description: 'Requests describes the minimum amount of compute
                      resources required. If Requests is omitted for a container,
                      it defaults to Limits if that is explicitly specified, otherwise
                      to an implementation-defined value. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                    type: object
                    additionalProperties:
                      pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                      anyOf:
                      - type: integer
                      - type: string
                      x-kubernetes-int-or-string: true
            storageDeviceSets:
              type: array
              items:
                description: StorageDeviceSet defines a set of storage devices. It
                  configures the StorageClassDeviceSets field in Rook-Ceph.
                type: object
                required:
                - count
                - dataPVCTemplate
                - name
                properties:
                  config:
                    description: 'StorageDeviceSetConfig defines Ceph OSD specific
                      config options for the StorageDeviceSet TODO: Fill in the members
                      when the actual configurable options are defined in rook-ceph'
                    type: object
                    properties:
                      tuneSlowDeviceClass:
                        description: TuneSlowDeviceClass tunes the OSD when running
                          on a slow Device Class
                        type: boolean
                  count:
                    description: Count is the number of devices in each StorageClassDeviceSet
                    type: integer
                    minimum: 1
                  dataPVCTemplate:
                    description: PersistentVolumeClaim is a user's request for and
                      claim to a persistent volume
                    type: object
                    properties:
                      apiVersion:
                        description: 'APIVersion defines the versioned schema of this
                          representation of an object. Servers should convert recognized
                          schemas to the latest internal value, and may reject unrecognized
                          values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
                        type: string
                      kind:
                        description: 'Kind is a string value representing the REST
                          resource this object represents. Servers may infer this
                          from the endpoint the client submits requests to. Cannot
                          be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
                        type: string
                      metadata:
                        description: 'Standard object''s metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata'
                        type: object
                      spec:
                        description: 'Spec defines the desired characteristics of
                          a volume requested by a pod author. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims'
                        type: object
                        properties:
                          accessModes:
                            description: 'AccessModes contains the desired access
                              modes the volume should have. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#access-modes-1'
                            type: array
                            items:
                              type: string
                          dataSource:
                            description: This field requires the VolumeSnapshotDataSource
                              alpha feature gate to be enabled and currently VolumeSnapshot
                              is the only supported data source. If the provisioner
                              can support VolumeSnapshot data source, it will create
                              a new volume and data will be restored to the volume
                              at the same time. If the provisioner does not support
                              VolumeSnapshot data source, volume will not be created
                              and the failure will be reported as an event. In the
                              future, we plan to support more data source types and
                              the behavior of the provisioner may change.
                            type: object
                            required:
                            - kind
                            - name
                            properties:
                              apiGroup:
                                description: APIGroup is the group for the resource
                                  being referenced. If APIGroup is not specified,
                                  the specified Kind must be in the core API group.
                                  For any other third-party types, APIGroup is required.
                                type: string
                              kind:
                                description: Kind is the type of resource being referenced
                                type: string
                              name:
                                description: Name is the name of resource being referenced
                                type: string
                          resources:
                            description: 'Resources represents the minimum resources
                              the volume should have. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#resources'
                            type: object
                            properties:
                              limits:
                                description: 'Limits describes the maximum amount
                                  of compute resources allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                                type: object
                                additionalProperties:
                                  pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                                  anyOf:
                                  - type: integer
                                  - type: string
                                  x-kubernetes-int-or-string: true
                              requests:
                                description: 'Requests describes the minimum amount
                                  of compute resources required. If Requests is omitted
                                  for a container, it defaults to Limits if that is
                                  explicitly specified, otherwise to an implementation-defined
                                  value. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                                type: object
                                additionalProperties:
                                  pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                                  anyOf:
                                  - type: integer
                                  - type: string
                                  x-kubernetes-int-or-string: true
                          selector:
                            description: A label query over volumes to consider for
                              binding.
                            type: object
                            properties:
                              matchExpressions:
                                description: matchExpressions is a list of label selector
                                  requirements. The requirements are ANDed.
                                type: array
                                items:
                                  description: A label selector requirement is a selector
                                    that contains values, a key, and an operator that
                                    relates the key and values.
                                  type: object
                                  required:
                                  - key
                                  - operator
                                  properties:
                                    key:
                                      description: key is the label key that the selector
                                        applies to.
                                      type: string
                                    operator:
                                      description: operator represents a key's relationship
                                        to a set of values. Valid operators are In,
                                        NotIn, Exists and DoesNotExist.
                                      type: string
                                    values:
                                      description: values is an array of string values.
                                        If the operator is In or NotIn, the values
                                        array must be non-empty. If the operator is
                                        Exists or DoesNotExist, the values array must
                                        be empty. This array is replaced during a
                                        strategic merge patch.
                                      type: array
                                      items:
                                        type: string
                              matchLabels:
                                description: matchLabels is a map of {key,value} pairs.
                                  A single {key,value} in the matchLabels map is equivalent
                                  to an element of matchExpressions, whose key field
                                  is "key", the operator is "In", and the values array
                                  contains only "value". The requirements are ANDed.
                                type: object
                                additionalProperties:
                                  type: string
                          storageClassName:
                            description: 'Name of the StorageClass required by the
                              claim. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#class-1'
                            type: string
                          volumeMode:
                            description: volumeMode defines what type of volume is
                              required by the claim. Value of Filesystem is implied
                              when not included in claim spec. This is a beta feature.
                            type: string
                          volumeName:
                            description: VolumeName is the binding reference to the
                              PersistentVolume backing this claim.
                            type: string
                      status:
                        description: 'Status represents the current information/status
                          of a persistent volume claim. Read-only. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims'
                        type: object
                        properties:
                          accessModes:
                            description: 'AccessModes contains the actual access modes
                              the volume backing the PVC has. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#access-modes-1'
                            type: array
                            items:
                              type: string
                          capacity:
                            description: Represents the actual resources of the underlying
                              volume.
                            type: object
                            additionalProperties:
                              pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                              anyOf:
                              - type: integer
                              - type: string
                              x-kubernetes-int-or-string: true
                          conditions:
                            description: Current Condition of persistent volume claim.
                              If underlying persistent volume is being resized then
                              the Condition will be set to 'ResizeStarted'.
                            type: array
                            items:
                              description: PersistentVolumeClaimCondition contails
                                details about state of pvc
                              type: object
                              required:
                              - status
                              - type
                              properties:
                                lastProbeTime:
                                  description: Last time we probed the condition.
                                  type: string
                                  format: date-time
                                lastTransitionTime:
                                  description: Last time the condition transitioned
                                    from one status to another.
                                  type: string
                                  format: date-time
                                message:
                                  description: Human-readable message indicating details
                                    about last transition.
                                  type: string
                                reason:
                                  description: Unique, this should be a short, machine
                                    understandable string that gives the reason for
                                    condition's last transition. If it reports "ResizeStarted"
                                    that means the underlying persistent volume is
                                    being resized.
                                  type: string
                                status:
                                  type: string
                                type:
                                  description: PersistentVolumeClaimConditionType
                                    is a valid value of PersistentVolumeClaimCondition.Type
                                  type: string
                          phase:
                            description: Phase represents the current phase of PersistentVolumeClaim.
                            type: string
                  metadataPVCTemplate:
                    description: PersistentVolumeClaim is a user's request for and
                      claim to a persistent volume
                    type: object
                    properties:
                      apiVersion:
                        description: 'APIVersion defines the versioned schema of this
                          representation of an object. Servers should convert recognized
                          schemas to the latest internal value, and may reject unrecognized
                          values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
                        type: string
                      kind:
                        description: 'Kind is a string value representing the REST
                          resource this object represents. Servers may infer this
                          from the endpoint the client submits requests to. Cannot
                          be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
                        type: string
                      metadata:
                        description: 'Standard object''s metadata. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata'
                        type: object
                      spec:
                        description: 'Spec defines the desired characteristics of
                          a volume requested by a pod author. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims'
                        type: object
                        properties:
                          accessModes:
                            description: 'AccessModes contains the desired access
                              modes the volume should have. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#access-modes-1'
                            type: array
                            items:
                              type: string
                          dataSource:
                            description: This field requires the VolumeSnapshotDataSource
                              alpha feature gate to be enabled and currently VolumeSnapshot
                              is the only supported data source. If the provisioner
                              can support VolumeSnapshot data source, it will create
                              a new volume and data will be restored to the volume
                              at the same time. If the provisioner does not support
                              VolumeSnapshot data source, volume will not be created
                              and the failure will be reported as an event. In the
                              future, we plan to support more data source types and
                              the behavior of the provisioner may change.
                            type: object
                            required:
                            - kind
                            - name
                            properties:
                              apiGroup:
                                description: APIGroup is the group for the resource
                                  being referenced. If APIGroup is not specified,
                                  the specified Kind must be in the core API group.
                                  For any other third-party types, APIGroup is required.
                                type: string
                              kind:
                                description: Kind is the type of resource being referenced
                                type: string
                              name:
                                description: Name is the name of resource being referenced
                                type: string
                          resources:
                            description: 'Resources represents the minimum resources
                              the volume should have. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#resources'
                            type: object
                            properties:
                              limits:
                                description: 'Limits describes the maximum amount
                                  of compute resources allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                                type: object
                                additionalProperties:
                                  pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                                  anyOf:
                                  - type: integer
                                  - type: string
                                  x-kubernetes-int-or-string: true
                              requests:
                                description: 'Requests describes the minimum amount
                                  of compute resources required. If Requests is omitted
                                  for a container, it defaults to Limits if that is
                                  explicitly specified, otherwise to an implementation-defined
                                  value. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                                type: object
                                additionalProperties:
                                  pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                                  anyOf:
                                  - type: integer
                                  - type: string
                                  x-kubernetes-int-or-string: true
                          selector:
                            description: A label query over volumes to consider for
                              binding.
                            type: object
                            properties:
                              matchExpressions:
                                description: matchExpressions is a list of label selector
                                  requirements. The requirements are ANDed.
                                type: array
                                items:
                                  description: A label selector requirement is a selector
                                    that contains values, a key, and an operator that
                                    relates the key and values.
                                  type: object
                                  required:
                                  - key
                                  - operator
                                  properties:
                                    key:
                                      description: key is the label key that the selector
                                        applies to.
                                      type: string
                                    operator:
                                      description: operator represents a key's relationship
                                        to a set of values. Valid operators are In,
                                        NotIn, Exists and DoesNotExist.
                                      type: string
                                    values:
                                      description: values is an array of string values.
                                        If the operator is In or NotIn, the values
                                        array must be non-empty. If the operator is
                                        Exists or DoesNotExist, the values array must
                                        be empty. This array is replaced during a
                                        strategic merge patch.
                                      type: array
                                      items:
                                        type: string
                              matchLabels:
                                description: matchLabels is a map of {key,value} pairs.
                                  A single {key,value} in the matchLabels map is equivalent
                                  to an element of matchExpressions, whose key field
                                  is "key", the operator is "In", and the values array
                                  contains only "value". The requirements are ANDed.
                                type: object
                                additionalProperties:
                                  type: string
                          storageClassName:
                            description: 'Name of the StorageClass required by the
                              claim. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#class-1'
                            type: string
                          volumeMode:
                            description: volumeMode defines what type of volume is
                              required by the claim. Value of Filesystem is implied
                              when not included in claim spec. This is a beta feature.
                            type: string
                          volumeName:
                            description: VolumeName is the binding reference to the
                              PersistentVolume backing this claim.
                            type: string
                      status:
                        description: 'Status represents the current information/status
                          of a persistent volume claim. Read-only. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims'
                        type: object
                        properties:
                          accessModes:
                            description: 'AccessModes contains the actual access modes
                              the volume backing the PVC has. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#access-modes-1'
                            type: array
                            items:
                              type: string
                          capacity:
                            description: Represents the actual resources of the underlying
                              volume.
                            type: object
                            additionalProperties:
                              pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                              anyOf:
                              - type: integer
                              - type: string
                              x-kubernetes-int-or-string: true
                          conditions:
                            description: Current Condition of persistent volume claim.
                              If underlying persistent volume is being resized then
                              the Condition will be set to 'ResizeStarted'.
                            type: array
                            items:
                              description: PersistentVolumeClaimCondition contails
                                details about state of pvc
                              type: object
                              required:
                              - status
                              - type
                              properties:
                                lastProbeTime:
                                  description: Last time we probed the condition.
                                  type: string
                                  format: date-time
                                lastTransitionTime:
                                  description: Last time the condition transitioned
                                    from one status to another.
                                  type: string
                                  format: date-time
                                message:
                                  description: Human-readable message indicating details
                                    about last transition.
                                  type: string
                                reason:
                                  description: Unique, this should be a short, machine
                                    understandable string that gives the reason for
                                    condition's last transition. If it reports "ResizeStarted"
                                    that means the underlying persistent volume is
                                    being resized.
                                  type: string
                                status:
                                  type: string
                                type:
                                  description: PersistentVolumeClaimConditionType
                                    is a valid value of PersistentVolumeClaimCondition.Type
                                  type: string
                          phase:
                            description: Phase represents the current phase of PersistentVolumeClaim.
                            type: string
                  name:
                    type: string
                  placement:
                    type: object
                    properties:
                      nodeAffinity:
                        description: Node affinity is a group of node affinity scheduling
                          rules.
                        type: object
                        properties:
                          preferredDuringSchedulingIgnoredDuringExecution:
                            description: The scheduler will prefer to schedule pods
                              to nodes that satisfy the affinity expressions specified
                              by this field, but it may choose a node that violates
                              one or more of the expressions. The node that is most
                              preferred is the one with the greatest sum of weights,
                              i.e. for each node that meets all of the scheduling
                              requirements (resource request, requiredDuringScheduling
                              affinity expressions, etc.), compute a sum by iterating
                              through the elements of this field and adding "weight"
                              to the sum if the node matches the corresponding matchExpressions;
                              the node(s) with the highest sum are the most preferred.
                            type: array
                            items:
                              description: An empty preferred scheduling term matches
                                all objects with implicit weight 0 (i.e. it's a no-op).
                                A null preferred scheduling term matches no objects
                                (i.e. is also a no-op).
                              type: object
                              required:
                              - preference
                              - weight
                              properties:
                                preference:
                                  description: A node selector term, associated with
                                    the corresponding weight.
                                  type: object
                                  properties:
                                    matchExpressions:
                                      description: A list of node selector requirements
                                        by node's labels.
                                      type: array
                                      items:
                                        description: A node selector requirement is
                                          a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: The label key that the selector
                                              applies to.
                                            type: string
                                          operator:
                                            description: Represents a key's relationship
                                              to a set of values. Valid operators
                                              are In, NotIn, Exists, DoesNotExist.
                                              Gt, and Lt.
                                            type: string
                                          values:
                                            description: An array of string values.
                                              If the operator is In or NotIn, the
                                              values array must be non-empty. If the
                                              operator is Exists or DoesNotExist,
                                              the values array must be empty. If the
                                              operator is Gt or Lt, the values array
                                              must have a single element, which will
                                              be interpreted as an integer. This array
                                              is replaced during a strategic merge
                                              patch.
                                            type: array
                                            items:
                                              type: string
                                    matchFields:
                                      description: A list of node selector requirements
                                        by node's fields.
                                      type: array
                                      items:
                                        description: A node selector requirement is
                                          a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: The label key that the selector
                                              applies to.
                                            type: string
                                          operator:
                                            description: Represents a key's relationship
                                              to a set of values. Valid operators
                                              are In, NotIn, Exists, DoesNotExist.
                                              Gt, and Lt.
                                            type: string
                                          values:
                                            description: An array of string values.
                                              If the operator is In or NotIn, the
                                              values array must be non-empty. If the
                                              operator is Exists or DoesNotExist,
                                              the values array must be empty. If the
                                              operator is Gt or Lt, the values array
                                              must have a single element, which will
                                              be interpreted as an integer. This array
                                              is replaced during a strategic merge
                                              patch.
                                            type: array
                                            items:
                                              type: string
                                weight:
                                  description: Weight associated with matching the
                                    corresponding nodeSelectorTerm, in the range 1-100.
                                  type: integer
                                  format: int32
                          requiredDuringSchedulingIgnoredDuringExecution:
                            description: If the affinity requirements specified by
                              this field are not met at scheduling time, the pod will
                              not be scheduled onto the node. If the affinity requirements
                              specified by this field cease to be met at some point
                              during pod execution (e.g. due to an update), the system
                              may or may not try to eventually evict the pod from
                              its node.
                            type: object
                            required:
                            - nodeSelectorTerms
                            properties:
                              nodeSelectorTerms:
                                description: Required. A list of node selector terms.
                                  The terms are ORed.
                                type: array
                                items:
                                  description: A null or empty node selector term
                                    matches no objects. The requirements of them are
                                    ANDed. The TopologySelectorTerm type implements
                                    a subset of the NodeSelectorTerm.
                                  type: object
                                  properties:
                                    matchExpressions:
                                      description: A list of node selector requirements
                                        by node's labels.
                                      type: array
                                      items:
                                        description: A node selector requirement is
                                          a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: The label key that the selector
                                              applies to.
                                            type: string
                                          operator:
                                            description: Represents a key's relationship
                                              to a set of values. Valid operators
                                              are In, NotIn, Exists, DoesNotExist.
                                              Gt, and Lt.
                                            type: string
                                          values:
                                            description: An array of string values.
                                              If the operator is In or NotIn, the
                                              values array must be non-empty. If the
                                              operator is Exists or DoesNotExist,
                                              the values array must be empty. If the
                                              operator is Gt or Lt, the values array
                                              must have a single element, which will
                                              be interpreted as an integer. This array
                                              is replaced during a strategic merge
                                              patch.
                                            type: array
                                            items:
                                              type: string
                                    matchFields:
                                      description: A list of node selector requirements
                                        by node's fields.
                                      type: array
                                      items:
                                        description: A node selector requirement is
                                          a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: The label key that the selector
                                              applies to.
                                            type: string
                                          operator:
                                            description: Represents a key's relationship
                                              to a set of values. Valid operators
                                              are In, NotIn, Exists, DoesNotExist.
                                              Gt, and Lt.
                                            type: string
                                          values:
                                            description: An array of string values.
                                              If the operator is In or NotIn, the
                                              values array must be non-empty. If the
                                              operator is Exists or DoesNotExist,
                                              the values array must be empty. If the
                                              operator is Gt or Lt, the values array
                                              must have a single element, which will
                                              be interpreted as an integer. This array
                                              is replaced during a strategic merge
                                              patch.
                                            type: array
                                            items:
                                              type: string
                      podAffinity:
                        description: Pod affinity is a group of inter pod affinity
                          scheduling rules.
                        type: object
                        properties:
                          preferredDuringSchedulingIgnoredDuringExecution:
                            description: The scheduler will prefer to schedule pods
                              to nodes that satisfy the affinity expressions specified
                              by this field, but it may choose a node that violates
                              one or more of the expressions. The node that is most
                              preferred is the one with the greatest sum of weights,
                              i.e. for each node that meets all of the scheduling
                              requirements (resource request, requiredDuringScheduling
                              affinity expressions, etc.), compute a sum by iterating
                              through the elements of this field and adding "weight"
                              to the sum if the node has pods which matches the corresponding
                              podAffinityTerm; the node(s) with the highest sum are
                              the most preferred.
                            type: array
                            items:
                              description: The weights of all of the matched WeightedPodAffinityTerm
                                fields are added per-node to find the most preferred
                                node(s)
                              type: object
                              required:
                              - podAffinityTerm
                              - weight
                              properties:
                                podAffinityTerm:
                                  description: Required. A pod affinity term, associated
                                    with the corresponding weight.
                                  type: object
                                  required:
                                  - topologyKey
                                  properties:
                                    labelSelector:
                                      description: A label query over a set of resources,
                                        in this case pods.
                                      type: object
                                      properties:
                                        matchExpressions:
                                          description: matchExpressions is a list
                                            of label selector requirements. The requirements
                                            are ANDed.
                                          type: array
                                          items:
                                            description: A label selector requirement
                                              is a selector that contains values,
                                              a key, and an operator that relates
                                              the key and values.
                                            type: object
                                            required:
                                            - key
                                            - operator
                                            properties:
                                              key:
                                                description: key is the label key
                                                  that the selector applies to.
                                                type: string
                                              operator:
                                                description: operator represents a
                                                  key's relationship to a set of values.
                                                  Valid operators are In, NotIn, Exists
                                                  and DoesNotExist.
                                                type: string
                                              values:
                                                description: values is an array of
                                                  string values. If the operator is
                                                  In or NotIn, the values array must
                                                  be non-empty. If the operator is
                                                  Exists or DoesNotExist, the values
                                                  array must be empty. This array
                                                  is replaced during a strategic merge
                                                  patch.
                                                type: array
                                                items:
                                                  type: string
                                        matchLabels:
                                          description: matchLabels is a map of {key,value}
                                            pairs. A single {key,value} in the matchLabels
                                            map is equivalent to an element of matchExpressions,
                                            whose key field is "key", the operator
                                            is "In", and the values array contains
                                            only "value". The requirements are ANDed.
                                          type: object
                                          additionalProperties:
                                            type: string
                                    namespaces:
                                      description: namespaces specifies which namespaces
                                        the labelSelector applies to (matches against);
                                        null or empty list means "this pod's namespace"
                                      type: array
                                      items:
                                        type: string
                                    topologyKey:
                                      description: This pod should be co-located (affinity)
                                        or not co-located (anti-affinity) with the
                                        pods matching the labelSelector in the specified
                                        namespaces, where co-located is defined as
                                        running on a node whose value of the label
                                        with key topologyKey matches that of any node
                                        on which any of the selected pods is running.
                                        Empty topologyKey is not allowed.
                                      type: string
                                weight:
                                  description: weight associated with matching the
                                    corresponding podAffinityTerm, in the range 1-100.
                                  type: integer
                                  format: int32
                          requiredDuringSchedulingIgnoredDuringExecution:
                            description: If the affinity requirements specified by
                              this field are not met at scheduling time, the pod will
                              not be scheduled onto the node. If the affinity requirements
                              specified by this field cease to be met at some point
                              during pod execution (e.g. due to a pod label update),
                              the system may or may not try to eventually evict the
                              pod from its node. When there are multiple elements,
                              the lists of nodes corresponding to each podAffinityTerm
                              are intersected, i.e. all terms must be satisfied.
                            type: array
                            items:
                              description: Defines a set of pods (namely those matching
                                the labelSelector relative to the given namespace(s))
                                that this pod should be co-located (affinity) or not
                                co-located (anti-affinity) with, where co-located
                                is defined as running on a node whose value of the
                                label with key <topologyKey> matches that of any node
                                on which a pod of the set of pods is running
                              type: object
                              required:
                              - topologyKey
                              properties:
                                labelSelector:
                                  description: A label query over a set of resources,
                                    in this case pods.
                                  type: object
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      type: array
                                      items:
                                        description: A label selector requirement
                                          is a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: operator represents a key's
                                              relationship to a set of values. Valid
                                              operators are In, NotIn, Exists and
                                              DoesNotExist.
                                            type: string
                                          values:
                                            description: values is an array of string
                                              values. If the operator is In or NotIn,
                                              the values array must be non-empty.
                                              If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This
                                              array is replaced during a strategic
                                              merge patch.
                                            type: array
                                            items:
                                              type: string
                                    matchLabels:
                                      description: matchLabels is a map of {key,value}
                                        pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions,
                                        whose key field is "key", the operator is
                                        "In", and the values array contains only "value".
                                        The requirements are ANDed.
                                      type: object
                                      additionalProperties:
                                        type: string
                                namespaces:
                                  description: namespaces specifies which namespaces
                                    the labelSelector applies to (matches against);
                                    null or empty list means "this pod's namespace"
                                  type: array
                                  items:
                                    type: string
                                topologyKey:
                                  description: This pod should be co-located (affinity)
                                    or not co-located (anti-affinity) with the pods
                                    matching the labelSelector in the specified namespaces,
                                    where co-located is defined as running on a node
                                    whose value of the label with key topologyKey
                                    matches that of any node on which any of the selected
                                    pods is running. Empty topologyKey is not allowed.
                                  type: string
                      podAntiAffinity:
                        description: Pod anti affinity is a group of inter pod anti
                          affinity scheduling rules.
                        type: object
                        properties:
                          preferredDuringSchedulingIgnoredDuringExecution:
                            description: The scheduler will prefer to schedule pods
                              to nodes that satisfy the anti-affinity expressions
                              specified by this field, but it may choose a node that
                              violates one or more of the expressions. The node that
                              is most preferred is the one with the greatest sum of
                              weights, i.e. for each node that meets all of the scheduling
                              requirements (resource request, requiredDuringScheduling
                              anti-affinity expressions, etc.), compute a sum by iterating
                              through the elements of this field and adding "weight"
                              to the sum if the node has pods which matches the corresponding
                              podAffinityTerm; the node(s) with the highest sum are
                              the most preferred.
                            type: array
                            items:
                              description: The weights of all of the matched WeightedPodAffinityTerm
                                fields are added per-node to find the most preferred
                                node(s)
                              type: object
                              required:
                              - podAffinityTerm
                              - weight
                              properties:
                                podAffinityTerm:
                                  description: Required. A pod affinity term, associated
                                    with the corresponding weight.
                                  type: object
                                  required:
                                  - topologyKey
                                  properties:
                                    labelSelector:
                                      description: A label query over a set of resources,
                                        in this case pods.
                                      type: object
                                      properties:
                                        matchExpressions:
                                          description: matchExpressions is a list
                                            of label selector requirements. The requirements
                                            are ANDed.
                                          type: array
                                          items:
                                            description: A label selector requirement
                                              is a selector that contains values,
                                              a key, and an operator that relates
                                              the key and values.
                                            type: object
                                            required:
                                            - key
                                            - operator
                                            properties:
                                              key:
                                                description: key is the label key
                                                  that the selector applies to.
                                                type: string
                                              operator:
                                                description: operator represents a
                                                  key's relationship to a set of values.
                                                  Valid operators are In, NotIn, Exists
                                                  and DoesNotExist.
                                                type: string
                                              values:
                                                description: values is an array of
                                                  string values. If the operator is
                                                  In or NotIn, the values array must
                                                  be non-empty. If the operator is
                                                  Exists or DoesNotExist, the values
                                                  array must be empty. This array
                                                  is replaced during a strategic merge
                                                  patch.
                                                type: array
                                                items:
                                                  type: string
                                        matchLabels:
                                          description: matchLabels is a map of {key,value}
                                            pairs. A single {key,value} in the matchLabels
                                            map is equivalent to an element of matchExpressions,
                                            whose key field is "key", the operator
                                            is "In", and the values array contains
                                            only "value". The requirements are ANDed.
                                          type: object
                                          additionalProperties:
                                            type: string
                                    namespaces:
                                      description: namespaces specifies which namespaces
                                        the labelSelector applies to (matches against);
                                        null or empty list means "this pod's namespace"
                                      type: array
                                      items:
                                        type: string
                                    topologyKey:
                                      description: This pod should be co-located (affinity)
                                        or not co-located (anti-affinity) with the
                                        pods matching the labelSelector in the specified
                                        namespaces, where co-located is defined as
                                        running on a node whose value of the label
                                        with key topologyKey matches that of any node
                                        on which any of the selected pods is running.
                                        Empty topologyKey is not allowed.
                                      type: string
                                weight:
                                  description: weight associated with matching the
                                    corresponding podAffinityTerm, in the range 1-100.
                                  type: integer
                                  format: int32
                          requiredDuringSchedulingIgnoredDuringExecution:
                            description: If the anti-affinity requirements specified
                              by this field are not met at scheduling time, the pod
                              will not be scheduled onto the node. If the anti-affinity
                              requirements specified by this field cease to be met
                              at some point during pod execution (e.g. due to a pod
                              label update), the system may or may not try to eventually
                              evict the pod from its node. When there are multiple
                              elements, the lists of nodes corresponding to each podAffinityTerm
                              are intersected, i.e. all terms must be satisfied.
                            type: array
                            items:
                              description: Defines a set of pods (namely those matching
                                the labelSelector relative to the given namespace(s))
                                that this pod should be co-located (affinity) or not
                                co-located (anti-affinity) with, where co-located
                                is defined as running on a node whose value of the
                                label with key <topologyKey> matches that of any node
                                on which a pod of the set of pods is running
                              type: object
                              required:
                              - topologyKey
                              properties:
                                labelSelector:
                                  description: A label query over a set of resources,
                                    in this case pods.
                                  type: object
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      type: array
                                      items:
                                        description: A label selector requirement
                                          is a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: operator represents a key's
                                              relationship to a set of values. Valid
                                              operators are In, NotIn, Exists and
                                              DoesNotExist.
                                            type: string
                                          values:
                                            description: values is an array of string
                                              values. If the operator is In or NotIn,
                                              the values array must be non-empty.
                                              If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This
                                              array is replaced during a strategic
                                              merge patch.
                                            type: array
                                            items:
                                              type: string
                                    matchLabels:
                                      description: matchLabels is a map of {key,value}
                                        pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions,
                                        whose key field is "key", the operator is
                                        "In", and the values array contains only "value".
                                        The requirements are ANDed.
                                      type: object
                                      additionalProperties:
                                        type: string
                                namespaces:
                                  description: namespaces specifies which namespaces
                                    the labelSelector applies to (matches against);
                                    null or empty list means "this pod's namespace"
                                  type: array
                                  items:
                                    type: string
                                topologyKey:
                                  description: This pod should be co-located (affinity)
                                    or not co-located (anti-affinity) with the pods
                                    matching the labelSelector in the specified namespaces,
                                    where co-located is defined as running on a node
                                    whose value of the label with key topologyKey
                                    matches that of any node on which any of the selected
                                    pods is running. Empty topologyKey is not allowed.
                                  type: string
                      tolerations:
                        type: array
                        items:
                          description: The pod this Toleration is attached to tolerates
                            any taint that matches the triple <key,value,effect> using
                            the matching operator <operator>.
                          type: object
                          properties:
                            effect:
                              description: Effect indicates the taint effect to match.
                                Empty means match all taint effects. When specified,
                                allowed values are NoSchedule, PreferNoSchedule and
                                NoExecute.
                              type: string
                            key:
                              description: Key is the taint key that the toleration
                                applies to. Empty means match all taint keys. If the
                                key is empty, operator must be Exists; this combination
                                means to match all values and all keys.
                              type: string
                            operator:
                              description: Operator represents a key's relationship
                                to the value. Valid operators are Exists and Equal.
                                Defaults to Equal. Exists is equivalent to wildcard
                                for value, so that a pod can tolerate all taints of
                                a particular category.
                              type: string
                            tolerationSeconds:
                              description: TolerationSeconds represents the period
                                of time the toleration (which must be of effect NoExecute,
                                otherwise this field is ignored) tolerates the taint.
                                By default, it is not set, which means tolerate the
                                taint forever (do not evict). Zero and negative values
                                will be treated as 0 (evict immediately) by the system.
                              type: integer
                              format: int64
                            value:
                              description: Value is the taint value the toleration
                                matches to. If the operator is Exists, the value should
                                be empty, otherwise just a regular string.
                              type: string
                      topologySpreadConstraints:
                        type: array
                        items:
                          description: TopologySpreadConstraint specifies how to spread
                            matching pods among the given topology.
                          type: object
                          required:
                          - maxSkew
                          - topologyKey
                          - whenUnsatisfiable
                          properties:
                            labelSelector:
                              description: LabelSelector is used to find matching
                                pods. Pods that match this label selector are counted
                                to determine the number of pods in their corresponding
                                topology domain.
                              type: object
                              properties:
                                matchExpressions:
                                  description: matchExpressions is a list of label
                                    selector requirements. The requirements are ANDed.
                                  type: array
                                  items:
                                    description: A label selector requirement is a
                                      selector that contains values, a key, and an
                                      operator that relates the key and values.
                                    type: object
                                    required:
                                    - key
                                    - operator
                                    properties:
                                      key:
                                        description: key is the label key that the
                                          selector applies to.
                                        type: string
                                      operator:
                                        description: operator represents a key's relationship
                                          to a set of values. Valid operators are
                                          In, NotIn, Exists and DoesNotExist.
                                        type: string
                                      values:
                                        description: values is an array of string
                                          values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the
                                          operator is Exists or DoesNotExist, the
                                          values array must be empty. This array is
                                          replaced during a strategic merge patch.
                                        type: array
                                        items:
                                          type: string
                                matchLabels:
                                  description: matchLabels is a map of {key,value}
                                    pairs. A single {key,value} in the matchLabels
                                    map is equivalent to an element of matchExpressions,
                                    whose key field is "key", the operator is "In",
                                    and the values array contains only "value". The
                                    requirements are ANDed.
                                  type: object
                                  additionalProperties:
                                    type: string
                            maxSkew:
                              description: 'MaxSkew describes the degree to which
                                pods may be unevenly distributed. It''s the maximum
                                permitted difference between the number of matching
                                pods in any two topology domains of a given topology
                                type. For example, in a 3-zone cluster, MaxSkew is
                                set to 1, and pods with the same labelSelector spread
                                as 1/1/0: | zone1 | zone2 | zone3 | |   P   |   P   |       |
                                - if MaxSkew is 1, incoming pod can only be scheduled
                                to zone3 to become 1/1/1; scheduling it onto zone1(zone2)
                                would make the ActualSkew(2-0) on zone1(zone2) violate
                                MaxSkew(1). - if MaxSkew is 2, incoming pod can be
                                scheduled onto any zone. It''s a required field. Default
                                value is 1 and 0 is not allowed.'
                              type: integer
                              format: int32
                            topologyKey:
                              description: TopologyKey is the key of node labels.
                                Nodes that have a label with this key and identical
                                values are considered to be in the same topology.
                                We consider each <key, value> as a "bucket", and try
                                to put balanced number of pods into each bucket. It's
                                a required field.
                              type: string
                            whenUnsatisfiable:
                              description: 'WhenUnsatisfiable indicates how to deal
                                with a pod if it doesn''t satisfy the spread constraint.
                                - DoNotSchedule (default) tells the scheduler not
                                to schedule it - ScheduleAnyway tells the scheduler
                                to still schedule it It''s considered as "Unsatisfiable"
                                if and only if placing incoming pod on any topology
                                violates "MaxSkew". For example, in a 3-zone cluster,
                                MaxSkew is set to 1, and pods with the same labelSelector
                                spread as 3/1/1: | zone1 | zone2 | zone3 | | P P P
                                |   P   |   P   | If WhenUnsatisfiable is set to DoNotSchedule,
                                incoming pod can only be scheduled to zone2(zone3)
                                to become 3/2/1(3/1/2) as ActualSkew(2-1) on zone2(zone3)
                                satisfies MaxSkew(1). In other words, the cluster
                                can still be imbalanced, but scheduler won''t make
                                it *more* imbalanced. It''s a required field.'
                              type: string
                  portable:
                    description: Portable says whether the OSDs in this device set
                      can move between nodes. This is ignored if Placement is not
                      set
                    type: boolean
                  preparePlacement:
                    type: object
                    properties:
                      nodeAffinity:
                        description: Node affinity is a group of node affinity scheduling
                          rules.
                        type: object
                        properties:
                          preferredDuringSchedulingIgnoredDuringExecution:
                            description: The scheduler will prefer to schedule pods
                              to nodes that satisfy the affinity expressions specified
                              by this field, but it may choose a node that violates
                              one or more of the expressions. The node that is most
                              preferred is the one with the greatest sum of weights,
                              i.e. for each node that meets all of the scheduling
                              requirements (resource request, requiredDuringScheduling
                              affinity expressions, etc.), compute a sum by iterating
                              through the elements of this field and adding "weight"
                              to the sum if the node matches the corresponding matchExpressions;
                              the node(s) with the highest sum are the most preferred.
                            type: array
                            items:
                              description: An empty preferred scheduling term matches
                                all objects with implicit weight 0 (i.e. it's a no-op).
                                A null preferred scheduling term matches no objects
                                (i.e. is also a no-op).
                              type: object
                              required:
                              - preference
                              - weight
                              properties:
                                preference:
                                  description: A node selector term, associated with
                                    the corresponding weight.
                                  type: object
                                  properties:
                                    matchExpressions:
                                      description: A list of node selector requirements
                                        by node's labels.
                                      type: array
                                      items:
                                        description: A node selector requirement is
                                          a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: The label key that the selector
                                              applies to.
                                            type: string
                                          operator:
                                            description: Represents a key's relationship
                                              to a set of values. Valid operators
                                              are In, NotIn, Exists, DoesNotExist.
                                              Gt, and Lt.
                                            type: string
                                          values:
                                            description: An array of string values.
                                              If the operator is In or NotIn, the
                                              values array must be non-empty. If the
                                              operator is Exists or DoesNotExist,
                                              the values array must be empty. If the
                                              operator is Gt or Lt, the values array
                                              must have a single element, which will
                                              be interpreted as an integer. This array
                                              is replaced during a strategic merge
                                              patch.
                                            type: array
                                            items:
                                              type: string
                                    matchFields:
                                      description: A list of node selector requirements
                                        by node's fields.
                                      type: array
                                      items:
                                        description: A node selector requirement is
                                          a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: The label key that the selector
                                              applies to.
                                            type: string
                                          operator:
                                            description: Represents a key's relationship
                                              to a set of values. Valid operators
                                              are In, NotIn, Exists, DoesNotExist.
                                              Gt, and Lt.
                                            type: string
                                          values:
                                            description: An array of string values.
                                              If the operator is In or NotIn, the
                                              values array must be non-empty. If the
                                              operator is Exists or DoesNotExist,
                                              the values array must be empty. If the
                                              operator is Gt or Lt, the values array
                                              must have a single element, which will
                                              be interpreted as an integer. This array
                                              is replaced during a strategic merge
                                              patch.
                                            type: array
                                            items:
                                              type: string
                                weight:
                                  description: Weight associated with matching the
                                    corresponding nodeSelectorTerm, in the range 1-100.
                                  type: integer
                                  format: int32
                          requiredDuringSchedulingIgnoredDuringExecution:
                            description: If the affinity requirements specified by
                              this field are not met at scheduling time, the pod will
                              not be scheduled onto the node. If the affinity requirements
                              specified by this field cease to be met at some point
                              during pod execution (e.g. due to an update), the system
                              may or may not try to eventually evict the pod from
                              its node.
                            type: object
                            required:
                            - nodeSelectorTerms
                            properties:
                              nodeSelectorTerms:
                                description: Required. A list of node selector terms.
                                  The terms are ORed.
                                type: array
                                items:
                                  description: A null or empty node selector term
                                    matches no objects. The requirements of them are
                                    ANDed. The TopologySelectorTerm type implements
                                    a subset of the NodeSelectorTerm.
                                  type: object
                                  properties:
                                    matchExpressions:
                                      description: A list of node selector requirements
                                        by node's labels.
                                      type: array
                                      items:
                                        description: A node selector requirement is
                                          a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: The label key that the selector
                                              applies to.
                                            type: string
                                          operator:
                                            description: Represents a key's relationship
                                              to a set of values. Valid operators
                                              are In, NotIn, Exists, DoesNotExist.
                                              Gt, and Lt.
                                            type: string
                                          values:
                                            description: An array of string values.
                                              If the operator is In or NotIn, the
                                              values array must be non-empty. If the
                                              operator is Exists or DoesNotExist,
                                              the values array must be empty. If the
                                              operator is Gt or Lt, the values array
                                              must have a single element, which will
                                              be interpreted as an integer. This array
                                              is replaced during a strategic merge
                                              patch.
                                            type: array
                                            items:
                                              type: string
                                    matchFields:
                                      description: A list of node selector requirements
                                        by node's fields.
                                      type: array
                                      items:
                                        description: A node selector requirement is
                                          a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: The label key that the selector
                                              applies to.
                                            type: string
                                          operator:
                                            description: Represents a key's relationship
                                              to a set of values. Valid operators
                                              are In, NotIn, Exists, DoesNotExist.
                                              Gt, and Lt.
                                            type: string
                                          values:
                                            description: An array of string values.
                                              If the operator is In or NotIn, the
                                              values array must be non-empty. If the
                                              operator is Exists or DoesNotExist,
                                              the values array must be empty. If the
                                              operator is Gt or Lt, the values array
                                              must have a single element, which will
                                              be interpreted as an integer. This array
                                              is replaced during a strategic merge
                                              patch.
                                            type: array
                                            items:
                                              type: string
                      podAffinity:
                        description: Pod affinity is a group of inter pod affinity
                          scheduling rules.
                        type: object
                        properties:
                          preferredDuringSchedulingIgnoredDuringExecution:
                            description: The scheduler will prefer to schedule pods
                              to nodes that satisfy the affinity expressions specified
                              by this field, but it may choose a node that violates
                              one or more of the expressions. The node that is most
                              preferred is the one with the greatest sum of weights,
                              i.e. for each node that meets all of the scheduling
                              requirements (resource request, requiredDuringScheduling
                              affinity expressions, etc.), compute a sum by iterating
                              through the elements of this field and adding "weight"
                              to the sum if the node has pods which matches the corresponding
                              podAffinityTerm; the node(s) with the highest sum are
                              the most preferred.
                            type: array
                            items:
                              description: The weights of all of the matched WeightedPodAffinityTerm
                                fields are added per-node to find the most preferred
                                node(s)
                              type: object
                              required:
                              - podAffinityTerm
                              - weight
                              properties:
                                podAffinityTerm:
                                  description: Required. A pod affinity term, associated
                                    with the corresponding weight.
                                  type: object
                                  required:
                                  - topologyKey
                                  properties:
                                    labelSelector:
                                      description: A label query over a set of resources,
                                        in this case pods.
                                      type: object
                                      properties:
                                        matchExpressions:
                                          description: matchExpressions is a list
                                            of label selector requirements. The requirements
                                            are ANDed.
                                          type: array
                                          items:
                                            description: A label selector requirement
                                              is a selector that contains values,
                                              a key, and an operator that relates
                                              the key and values.
                                            type: object
                                            required:
                                            - key
                                            - operator
                                            properties:
                                              key:
                                                description: key is the label key
                                                  that the selector applies to.
                                                type: string
                                              operator:
                                                description: operator represents a
                                                  key's relationship to a set of values.
                                                  Valid operators are In, NotIn, Exists
                                                  and DoesNotExist.
                                                type: string
                                              values:
                                                description: values is an array of
                                                  string values. If the operator is
                                                  In or NotIn, the values array must
                                                  be non-empty. If the operator is
                                                  Exists or DoesNotExist, the values
                                                  array must be empty. This array
                                                  is replaced during a strategic merge
                                                  patch.
                                                type: array
                                                items:
                                                  type: string
                                        matchLabels:
                                          description: matchLabels is a map of {key,value}
                                            pairs. A single {key,value} in the matchLabels
                                            map is equivalent to an element of matchExpressions,
                                            whose key field is "key", the operator
                                            is "In", and the values array contains
                                            only "value". The requirements are ANDed.
                                          type: object
                                          additionalProperties:
                                            type: string
                                    namespaces:
                                      description: namespaces specifies which namespaces
                                        the labelSelector applies to (matches against);
                                        null or empty list means "this pod's namespace"
                                      type: array
                                      items:
                                        type: string
                                    topologyKey:
                                      description: This pod should be co-located (affinity)
                                        or not co-located (anti-affinity) with the
                                        pods matching the labelSelector in the specified
                                        namespaces, where co-located is defined as
                                        running on a node whose value of the label
                                        with key topologyKey matches that of any node
                                        on which any of the selected pods is running.
                                        Empty topologyKey is not allowed.
                                      type: string
                                weight:
                                  description: weight associated with matching the
                                    corresponding podAffinityTerm, in the range 1-100.
                                  type: integer
                                  format: int32
                          requiredDuringSchedulingIgnoredDuringExecution:
                            description: If the affinity requirements specified by
                              this field are not met at scheduling time, the pod will
                              not be scheduled onto the node. If the affinity requirements
                              specified by this field cease to be met at some point
                              during pod execution (e.g. due to a pod label update),
                              the system may or may not try to eventually evict the
                              pod from its node. When there are multiple elements,
                              the lists of nodes corresponding to each podAffinityTerm
                              are intersected, i.e. all terms must be satisfied.
                            type: array
                            items:
                              description: Defines a set of pods (namely those matching
                                the labelSelector relative to the given namespace(s))
                                that this pod should be co-located (affinity) or not
                                co-located (anti-affinity) with, where co-located
                                is defined as running on a node whose value of the
                                label with key <topologyKey> matches that of any node
                                on which a pod of the set of pods is running
                              type: object
                              required:
                              - topologyKey
                              properties:
                                labelSelector:
                                  description: A label query over a set of resources,
                                    in this case pods.
                                  type: object
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      type: array
                                      items:
                                        description: A label selector requirement
                                          is a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: operator represents a key's
                                              relationship to a set of values. Valid
                                              operators are In, NotIn, Exists and
                                              DoesNotExist.
                                            type: string
                                          values:
                                            description: values is an array of string
                                              values. If the operator is In or NotIn,
                                              the values array must be non-empty.
                                              If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This
                                              array is replaced during a strategic
                                              merge patch.
                                            type: array
                                            items:
                                              type: string
                                    matchLabels:
                                      description: matchLabels is a map of {key,value}
                                        pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions,
                                        whose key field is "key", the operator is
                                        "In", and the values array contains only "value".
                                        The requirements are ANDed.
                                      type: object
                                      additionalProperties:
                                        type: string
                                namespaces:
                                  description: namespaces specifies which namespaces
                                    the labelSelector applies to (matches against);
                                    null or empty list means "this pod's namespace"
                                  type: array
                                  items:
                                    type: string
                                topologyKey:
                                  description: This pod should be co-located (affinity)
                                    or not co-located (anti-affinity) with the pods
                                    matching the labelSelector in the specified namespaces,
                                    where co-located is defined as running on a node
                                    whose value of the label with key topologyKey
                                    matches that of any node on which any of the selected
                                    pods is running. Empty topologyKey is not allowed.
                                  type: string
                      podAntiAffinity:
                        description: Pod anti affinity is a group of inter pod anti
                          affinity scheduling rules.
                        type: object
                        properties:
                          preferredDuringSchedulingIgnoredDuringExecution:
                            description: The scheduler will prefer to schedule pods
                              to nodes that satisfy the anti-affinity expressions
                              specified by this field, but it may choose a node that
                              violates one or more of the expressions. The node that
                              is most preferred is the one with the greatest sum of
                              weights, i.e. for each node that meets all of the scheduling
                              requirements (resource request, requiredDuringScheduling
                              anti-affinity expressions, etc.), compute a sum by iterating
                              through the elements of this field and adding "weight"
                              to the sum if the node has pods which matches the corresponding
                              podAffinityTerm; the node(s) with the highest sum are
                              the most preferred.
                            type: array
                            items:
                              description: The weights of all of the matched WeightedPodAffinityTerm
                                fields are added per-node to find the most preferred
                                node(s)
                              type: object
                              required:
                              - podAffinityTerm
                              - weight
                              properties:
                                podAffinityTerm:
                                  description: Required. A pod affinity term, associated
                                    with the corresponding weight.
                                  type: object
                                  required:
                                  - topologyKey
                                  properties:
                                    labelSelector:
                                      description: A label query over a set of resources,
                                        in this case pods.
                                      type: object
                                      properties:
                                        matchExpressions:
                                          description: matchExpressions is a list
                                            of label selector requirements. The requirements
                                            are ANDed.
                                          type: array
                                          items:
                                            description: A label selector requirement
                                              is a selector that contains values,
                                              a key, and an operator that relates
                                              the key and values.
                                            type: object
                                            required:
                                            - key
                                            - operator
                                            properties:
                                              key:
                                                description: key is the label key
                                                  that the selector applies to.
                                                type: string
                                              operator:
                                                description: operator represents a
                                                  key's relationship to a set of values.
                                                  Valid operators are In, NotIn, Exists
                                                  and DoesNotExist.
                                                type: string
                                              values:
                                                description: values is an array of
                                                  string values. If the operator is
                                                  In or NotIn, the values array must
                                                  be non-empty. If the operator is
                                                  Exists or DoesNotExist, the values
                                                  array must be empty. This array
                                                  is replaced during a strategic merge
                                                  patch.
                                                type: array
                                                items:
                                                  type: string
                                        matchLabels:
                                          description: matchLabels is a map of {key,value}
                                            pairs. A single {key,value} in the matchLabels
                                            map is equivalent to an element of matchExpressions,
                                            whose key field is "key", the operator
                                            is "In", and the values array contains
                                            only "value". The requirements are ANDed.
                                          type: object
                                          additionalProperties:
                                            type: string
                                    namespaces:
                                      description: namespaces specifies which namespaces
                                        the labelSelector applies to (matches against);
                                        null or empty list means "this pod's namespace"
                                      type: array
                                      items:
                                        type: string
                                    topologyKey:
                                      description: This pod should be co-located (affinity)
                                        or not co-located (anti-affinity) with the
                                        pods matching the labelSelector in the specified
                                        namespaces, where co-located is defined as
                                        running on a node whose value of the label
                                        with key topologyKey matches that of any node
                                        on which any of the selected pods is running.
                                        Empty topologyKey is not allowed.
                                      type: string
                                weight:
                                  description: weight associated with matching the
                                    corresponding podAffinityTerm, in the range 1-100.
                                  type: integer
                                  format: int32
                          requiredDuringSchedulingIgnoredDuringExecution:
                            description: If the anti-affinity requirements specified
                              by this field are not met at scheduling time, the pod
                              will not be scheduled onto the node. If the anti-affinity
                              requirements specified by this field cease to be met
                              at some point during pod execution (e.g. due to a pod
                              label update), the system may or may not try to eventually
                              evict the pod from its node. When there are multiple
                              elements, the lists of nodes corresponding to each podAffinityTerm
                              are intersected, i.e. all terms must be satisfied.
                            type: array
                            items:
                              description: Defines a set of pods (namely those matching
                                the labelSelector relative to the given namespace(s))
                                that this pod should be co-located (affinity) or not
                                co-located (anti-affinity) with, where co-located
                                is defined as running on a node whose value of the
                                label with key <topologyKey> matches that of any node
                                on which a pod of the set of pods is running
                              type: object
                              required:
                              - topologyKey
                              properties:
                                labelSelector:
                                  description: A label query over a set of resources,
                                    in this case pods.
                                  type: object
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      type: array
                                      items:
                                        description: A label selector requirement
                                          is a selector that contains values, a key,
                                          and an operator that relates the key and
                                          values.
                                        type: object
                                        required:
                                        - key
                                        - operator
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: operator represents a key's
                                              relationship to a set of values. Valid
                                              operators are In, NotIn, Exists and
                                              DoesNotExist.
                                            type: string
                                          values:
                                            description: values is an array of string
                                              values. If the operator is In or NotIn,
                                              the values array must be non-empty.
                                              If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This
                                              array is replaced during a strategic
                                              merge patch.
                                            type: array
                                            items:
                                              type: string
                                    matchLabels:
                                      description: matchLabels is a map of {key,value}
                                        pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions,
                                        whose key field is "key", the operator is
                                        "In", and the values array contains only "value".
                                        The requirements are ANDed.
                                      type: object
                                      additionalProperties:
                                        type: string
                                namespaces:
                                  description: namespaces specifies which namespaces
                                    the labelSelector applies to (matches against);
                                    null or empty list means "this pod's namespace"
                                  type: array
                                  items:
                                    type: string
                                topologyKey:
                                  description: This pod should be co-located (affinity)
                                    or not co-located (anti-affinity) with the pods
                                    matching the labelSelector in the specified namespaces,
                                    where co-located is defined as running on a node
                                    whose value of the label with key topologyKey
                                    matches that of any node on which any of the selected
                                    pods is running. Empty topologyKey is not allowed.
                                  type: string
                      tolerations:
                        type: array
                        items:
                          description: The pod this Toleration is attached to tolerates
                            any taint that matches the triple <key,value,effect> using
                            the matching operator <operator>.
                          type: object
                          properties:
                            effect:
                              description: Effect indicates the taint effect to match.
                                Empty means match all taint effects. When specified,
                                allowed values are NoSchedule, PreferNoSchedule and
                                NoExecute.
                              type: string
                            key:
                              description: Key is the taint key that the toleration
                                applies to. Empty means match all taint keys. If the
                                key is empty, operator must be Exists; this combination
                                means to match all values and all keys.
                              type: string
                            operator:
                              description: Operator represents a key's relationship
                                to the value. Valid operators are Exists and Equal.
                                Defaults to Equal. Exists is equivalent to wildcard
                                for value, so that a pod can tolerate all taints of
                                a particular category.
                              type: string
                            tolerationSeconds:
                              description: TolerationSeconds represents the period
                                of time the toleration (which must be of effect NoExecute,
                                otherwise this field is ignored) tolerates the taint.
                                By default, it is not set, which means tolerate the
                                taint forever (do not evict). Zero and negative values
                                will be treated as 0 (evict immediately) by the system.
                              type: integer
                              format: int64
                            value:
                              description: Value is the taint value the toleration
                                matches to. If the operator is Exists, the value should
                                be empty, otherwise just a regular string.
                              type: string
                      topologySpreadConstraints:
                        type: array
                        items:
                          description: TopologySpreadConstraint specifies how to spread
                            matching pods among the given topology.
                          type: object
                          required:
                          - maxSkew
                          - topologyKey
                          - whenUnsatisfiable
                          properties:
                            labelSelector:
                              description: LabelSelector is used to find matching
                                pods. Pods that match this label selector are counted
                                to determine the number of pods in their corresponding
                                topology domain.
                              type: object
                              properties:
                                matchExpressions:
                                  description: matchExpressions is a list of label
                                    selector requirements. The requirements are ANDed.
                                  type: array
                                  items:
                                    description: A label selector requirement is a
                                      selector that contains values, a key, and an
                                      operator that relates the key and values.
                                    type: object
                                    required:
                                    - key
                                    - operator
                                    properties:
                                      key:
                                        description: key is the label key that the
                                          selector applies to.
                                        type: string
                                      operator:
                                        description: operator represents a key's relationship
                                          to a set of values. Valid operators are
                                          In, NotIn, Exists and DoesNotExist.
                                        type: string
                                      values:
                                        description: values is an array of string
                                          values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the
                                          operator is Exists or DoesNotExist, the
                                          values array must be empty. This array is
                                          replaced during a strategic merge patch.
                                        type: array
                                        items:
                                          type: string
                                matchLabels:
                                  description: matchLabels is a map of {key,value}
                                    pairs. A single {key,value} in the matchLabels
                                    map is equivalent to an element of matchExpressions,
                                    whose key field is "key", the operator is "In",
                                    and the values array contains only "value". The
                                    requirements are ANDed.
                                  type: object
                                  additionalProperties:
                                    type: string
                            maxSkew:
                              description: 'MaxSkew describes the degree to which
                                pods may be unevenly distributed. It''s the maximum
                                permitted difference between the number of matching
                                pods in any two topology domains of a given topology
                                type. For example, in a 3-zone cluster, MaxSkew is
                                set to 1, and pods with the same labelSelector spread
                                as 1/1/0: | zone1 | zone2 | zone3 | |   P   |   P   |       |
                                - if MaxSkew is 1, incoming pod can only be scheduled
                                to zone3 to become 1/1/1; scheduling it onto zone1(zone2)
                                would make the ActualSkew(2-0) on zone1(zone2) violate
                                MaxSkew(1). - if MaxSkew is 2, incoming pod can be
                                scheduled onto any zone. It''s a required field. Default
                                value is 1 and 0 is not allowed.'
                              type: integer
                              format: int32
                            topologyKey:
                              description: TopologyKey is the key of node labels.
                                Nodes that have a label with this key and identical
                                values are considered to be in the same topology.
                                We consider each <key, value> as a "bucket", and try
                                to put balanced number of pods into each bucket. It's
                                a required field.
                              type: string
                            whenUnsatisfiable:
                              description: 'WhenUnsatisfiable indicates how to deal
                                with a pod if it doesn''t satisfy the spread constraint.
                                - DoNotSchedule (default) tells the scheduler not
                                to schedule it - ScheduleAnyway tells the scheduler
                                to still schedule it It''s considered as "Unsatisfiable"
                                if and only if placing incoming pod on any topology
                                violates "MaxSkew". For example, in a 3-zone cluster,
                                MaxSkew is set to 1, and pods with the same labelSelector
                                spread as 3/1/1: | zone1 | zone2 | zone3 | | P P P
                                |   P   |   P   | If WhenUnsatisfiable is set to DoNotSchedule,
                                incoming pod can only be scheduled to zone2(zone3)
                                to become 3/2/1(3/1/2) as ActualSkew(2-1) on zone2(zone3)
                                satisfies MaxSkew(1). In other words, the cluster
                                can still be imbalanced, but scheduler won''t make
                                it *more* imbalanced. It''s a required field.'
                              type: string
                  replica:
                    description: Replica is the number of StorageClassDeviceSets for
                      this StorageDeviceSet
                    type: integer
                    minimum: 1
                  resources:
                    description: ResourceRequirements describes the compute resource
                      requirements.
                    type: object
                    properties:
                      limits:
                        description: 'Limits describes the maximum amount of compute
                          resources allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                        type: object
                        additionalProperties:
                          pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                          anyOf:
                          - type: integer
                          - type: string
                          x-kubernetes-int-or-string: true
                      requests:
                        description: 'Requests describes the minimum amount of compute
                          resources required. If Requests is omitted for a container,
                          it defaults to Limits if that is explicitly specified, otherwise
                          to an implementation-defined value. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/'
                        type: object
                        additionalProperties:
                          pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                          anyOf:
                          - type: integer
                          - type: string
                          x-kubernetes-int-or-string: true
                  topologyKey:
                    description: TopologyKey is the Kubernetes topology label that
                      the StorageClassDeviceSets will be distributed across. Ignored
                      if Placement is set
                    type: string
            version:
              description: Version specifies the version of StorageCluster
              type: string
        status:
          description: StorageClusterStatus defines the observed state of StorageCluster
          type: object
          properties:
            conditions:
              description: Conditions describes the state of the StorageCluster resource.
              type: array
              items:
                description: Condition represents the state of the operator's reconciliation
                  functionality.
                type: object
                required:
                - status
                - type
                properties:
                  lastHeartbeatTime:
                    type: string
                    format: date-time
                  lastTransitionTime:
                    type: string
                    format: date-time
                  message:
                    type: string
                  reason:
                    type: string
                  status:
                    type: string
                  type:
                    description: ConditionType is the state of the operator's reconciliation
                      functionality.
                    type: string
            externalSecretHash:
              description: ExternalSecretHash holds the checksum value of external
                secret data.
              type: string
            failureDomain:
              description: FailureDomain is the base CRUSH element Ceph will use to
                distribute its data replicas for the default CephBlockPool
              type: string
            nodeTopologies:
              description: NodeTopologies is a list of topology labels on all nodes
                matching the StorageCluster's placement selector.
              type: object
              properties:
                labels:
                  description: Labels is a map of topology label keys (e.g. "failure-domain.kubernetes.io")
                    to a set of values for those keys.
                  type: object
                  additionalProperties:
                    description: TopologyLabelValues is a list of values for a topology
                      label
                    type: array
                    items:
                      type: string
                  nullable: true
            phase:
              description: Phase describes the Phase of StorageCluster This is used
                by OLM UI to provide status information to the user
              type: string
            relatedObjects:
              description: RelatedObjects is a list of objects created and maintained
                by this operator. Object references will be added to this list after
                they have been created AND found in the cluster.
              type: array
              items:
                description: ObjectReference contains enough information to let you
                  inspect or modify the referred object.
                type: object
                properties:
                  apiVersion:
                    description: API version of the referent.
                    type: string
                  fieldPath:
                    description: 'If referring to a piece of an object instead of
                      an entire object, this string should contain a valid JSON/Go
                      field access statement, such as desiredState.manifest.containers[2].
                      For example, if the object reference is to a container within
                      a pod, this would take on a value like: "spec.containers{name}"
                      (where "name" refers to the name of the container that triggered
                      the event) or if no container name is specified "spec.containers[2]"
                      (container with index 2 in this pod). This syntax is chosen
                      only to have some well-defined way of referencing a part of
                      an object. TODO: this design is not final and this field is
                      subject to change in the future.'
                    type: string
                  kind:
                    description: 'Kind of the referent. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
                    type: string
                  name:
                    description: 'Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names'
                    type: string
                  namespace:
                    description: 'Namespace of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/'
                    type: string
                  resourceVersion:
                    description: 'Specific resourceVersion to which this reference
                      is made, if any. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#concurrency-control-and-consistency'
                    type: string
                  uid:
                    description: 'UID of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#uids'
                    type: string
  version: v1
  versions:
  - name: v1
    served: true
    storage: true
`
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
      replica: 3
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

    replica: 3
`
