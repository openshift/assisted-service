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

func Manifests() (map[string][]byte, error) {
	lsoSubs, err := lsoSubscription()
	if err != nil {
		return nil, err
	}
	manifests := make(map[string][]byte)
	manifests["99_openshift-lso_ns.yaml"] = []byte(localStorageNamespace)
	manifests["99_openshift-lso_operator_group.yaml"] = []byte(lsoOperatorGroup)
	manifests["99_openshift-lso_subscription.yaml"] = lsoSubs
	manifests["99_openshift-lso_lvset_cr.yaml"] = []byte(localVolumeSet)
	manifests["99_openshift-lso_lvset_crd.yaml"] = []byte(localVolumeSetCrd)
	return manifests, nil
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
  name: openshift-local-storage`

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

const localVolumeSetCrd = `
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: localvolumesets.local.storage.openshift.io
spec:
  additionalPrinterColumns:
  - JSONPath: .spec.storageClassName
    description: StorageClass
    name: StorageClass
    type: string
  - JSONPath: .status.totalProvisionedDeviceCount
    description: The number of PVs provisioned for this LocalVolumeSet's StorageClass
    name: Provisioned
    type: integer
  - JSONPath: .metadata.creationTimestamp
    name: Age
    type: date
  group: local.storage.openshift.io
  names:
    kind: LocalVolumeSet
    listKind: LocalVolumeSetList
    plural: localvolumesets
    singular: localvolumeset
    shortNames:
    - lvset
    - lvsets
  scope: Namespaced
  preserveUnknownFields: false
  subresources:
    status: {}
  validation:
    openAPIV3Schema:
      required:
          - spec
      type: object
      description: LocalVolumeSet enables automatic provisioning of local PersistentVolumes based on specified
        criteria.
      properties:
        spec:
          description: LocalVolumeSetSpec defines the desired state of LocalVolumeSet
          properties:
            deviceInclusionSpec:
              description: DeviceInclusionSpec is the filtration rule for including
                a device in the device discovery
              properties:
                deviceMechanicalProperties:
                  description: DeviceMechanicalProperty denotes whether Rotational
                    or NonRotational disks should be used. by default, it selects
                    both
                  items:
                    description: DeviceMechanicalProperty holds the device's mechanical
                      spec. It can be rotational or nonRotational
                    type: string
                  type: array
                deviceTypes:
                  description: 'Devices is the list of devices that should be used
                    for automatic detection. This would be one of the types supported
                    by the local-storage operator. Currently, the supported types
                    are: disk, part. If the list is empty no devices will be selected.'
                  items:
                    description: DeviceType is the types that will be supported by
                      the LSO.
                    type: string
                    enum:
                      - disk
                      - part
                  type: array
                maxSize:
                  description: MaxSize is the maximum size of the device which needs
                    to be included
                  type: string
                minSize:
                  description: MinSize is the minimum size of the device which needs
                    to be included
                  type: string
                models:
                  description: Models is a list of device models. If not empty, the
                    device's model as outputted by lsblk needs to contain at least
                    one of these strings.
                  items:
                    type: string
                  type: array
                vendors:
                  description: Vendors is a list of device vendors. If not empty,
                    the device's model as outputted by lsblk needs to contain at least
                    one of these strings.
                  items:
                    type: string
                  type: array
              type: object
            maxDeviceCount:
              description: Maximum number of Devices that needs to be detected per
                node. If omitted, there will be no maximum.
              format: int32
              type: integer
            fsType:
              description: FSType type to create when volumeMode is Filesystem
              type: string
            nodeSelector:
              description: Nodes on which the automatic detection policies must run.
              properties:
                nodeSelectorTerms:
                  description: Required. A list of node selector terms. The terms
                    are ORed.
                  items:
                    description: A null or empty node selector term matches no objects.
                      The requirements of them are ANDed. The TopologySelectorTerm
                      type implements a subset of the NodeSelectorTerm.
                    properties:
                      matchExpressions:
                        description: A list of node selector requirements by node's
                          labels.
                        items:
                          description: A node selector requirement is a selector that
                            contains values, a key, and an operator that relates the
                            key and values.
                          properties:
                            key:
                              description: The label key that the selector applies
                                to.
                              type: string
                            operator:
                              description: Represents a key's relationship to a set
                                of values. Valid operators are In, NotIn, Exists,
                                DoesNotExist. Gt, and Lt.
                              type: string
                            values:
                              description: An array of string values. If the operator
                                is In or NotIn, the values array must be non-empty.
                                If the operator is Exists or DoesNotExist, the values
                                array must be empty. If the operator is Gt or Lt,
                                the values array must have a single element, which
                                will be interpreted as an integer. This array is replaced
                                during a strategic merge patch.
                              items:
                                type: string
                              type: array
                          required:
                          - key
                          - operator
                          type: object
                        type: array
                      matchFields:
                        description: A list of node selector requirements by node's
                          fields.
                        items:
                          description: A node selector requirement is a selector that
                            contains values, a key, and an operator that relates the
                            key and values.
                          properties:
                            key:
                              description: The label key that the selector applies
                                to.
                              type: string
                            operator:
                              description: Represents a key's relationship to a set
                                of values. Valid operators are In, NotIn, Exists,
                                DoesNotExist. Gt, and Lt.
                              type: string
                            values:
                              description: An array of string values. If the operator
                                is In or NotIn, the values array must be non-empty.
                                If the operator is Exists or DoesNotExist, the values
                                array must be empty. If the operator is Gt or Lt,
                                the values array must have a single element, which
                                will be interpreted as an integer. This array is replaced
                                during a strategic merge patch.
                              items:
                                type: string
                              type: array
                          required:
                          - key
                          - operator
                          type: object
                        type: array
                    type: object
                  type: array
              required:
              - nodeSelectorTerms
              type: object
            storageClassName:
              description: StorageClassName to use for set of matched devices
              type: string
            tolerations:
              description: If specified, a list of tolerations to pass to the discovery
                daemons.
              items:
                description: The pod this Toleration is attached to tolerates any
                  taint that matches the triple <key,value,effect> using the matching
                  operator <operator>.
                properties:
                  effect:
                    description: Effect indicates the taint effect to match. Empty
                      means match all taint effects. When specified, allowed values
                      are NoSchedule, PreferNoSchedule and NoExecute.
                    type: string
                  key:
                    description: Key is the taint key that the toleration applies
                      to. Empty means match all taint keys. If the key is empty, operator
                      must be Exists; this combination means to match all values and
                      all keys.
                    type: string
                  operator:
                    description: Operator represents a key's relationship to the value.
                      Valid operators are Exists and Equal. Defaults to Equal. Exists
                      is equivalent to wildcard for value, so that a pod can tolerate
                      all taints of a particular category.
                    type: string
                  tolerationSeconds:
                    description: TolerationSeconds represents the period of time the
                      toleration (which must be of effect NoExecute, otherwise this
                      field is ignored) tolerates the taint. By default, it is not
                      set, which means tolerate the taint forever (do not evict).
                      Zero and negative values will be treated as 0 (evict immediately)
                      by the system.
                    format: int64
                    type: integer
                  value:
                    description: Value is the taint value the toleration matches to.
                      If the operator is Exists, the value should be empty, otherwise
                      just a regular string.
                    type: string
                type: object
              type: array
            volumeMode:
              description: VolumeMode determines whether the PV created is Block or
                Filesystem. It will default to Filesystem
              type: string
              enum:
                - Block
                - Filesystem
          required:
          - storageClassName
          type: object
        status:
          description: LocalVolumeSetStatus defines the observed state of LocalVolumeSet
          properties:
            conditions:
              description: Conditions is a list of conditions and their status.
              items:
                description: OperatorCondition is just the standard condition fields.
                properties:
                  lastTransitionTime:
                    format: date-time
                    type: string
                  message:
                    type: string
                  reason:
                    type: string
                  status:
                    type: string
                  type:
                    type: string
                type: object
              type: array
            observedGeneration:
              description: observedGeneration is the last generation change the operator
                has dealt with
              format: int64
              type: integer
            totalProvisionedDeviceCount:
              description: TotalProvisionedDeviceCount is the count of the total devices
                over which the PVs has been provisioned
              format: int32
              type: integer
          type: object
  version: v1alpha1
  versions:
  - name: v1alpha1
    served: true
    storage: true`
