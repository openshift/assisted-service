# OLM operator plugins development

## Existing plugins
  - [AMD GPU Operator (AMDGPU)](../../internal/operators/amdgpu)
  - [Authorino](../../internal/operators/authorino)
  - [OpenShift Virtualization (CNV)](../../internal/operators/cnv)
  - [Fence Agents Remediation (FAR)](../../internal/operators/fenceagentsremediation)
  - [Kernel Module Management (KMM)](../../internal/operators/kmm)
  - [Local Storage Operator (LSO)](../../internal/operators/lso)
  - [Logical Volume Manager (LVM)](../../internal/operators/lvm)
  - [Multi-Cluster Engine (MCE)](../../internal/operators/mce)
  - [Migration Toolkit for Virtualization (MTV)](../../internal/operators/mtv)
  - [NMState](../../internal/operators/nmstate)
  - [Node Feature Discovery](../../internal/operators/nodefeaturediscovery)
  - [Node Health Check Operator(NHO)](../../internal/operators/nodehealthcheck)
  - [Node Maintenance](../../internal/operators/nodemaintenance)
  - [NVIDIA GPU Operator](../../internal/operators/nvidiagpu)
  - [OpenShift Data Foundation (ODF)](../../internal/operators/odf)
  - [OpenShift AI](../../internal/operators/openshiftai)
  - [OpenShift Service Mesh (OSC)](../../internal/operators/osc)
  - [OpenShift Pipelines](../../internal/operators/pipelines)
  - [Self Node Remediation (SNR)](../../internal/operators/selfnoderemediation)
  - [OpenShift Serverless](../../internal/operators/serverless)
  - [OpenShift Service Mesh](../../internal/operators/servicemesh)

## How to implement a new OLM operator plugin

To implement support for a new OLM operator plugin you need to make following changes:

 1. Introduce new validation IDs for the new operator in the [swagger specification](../../swagger.yaml):
    - for host validation:
      ```yaml
      host-validation-id:
        type: string
        enum:
          - 'connected'
          ...
          - 'lso-requirements-satisfied'
          - 'cnv-requirements-satisfied'
          - 'odf-requirements-satisfied'
          - 'lvm-requirements-satisfied'
      ```
    - for cluster validation:
      ```yaml
      cluster-validation-id:
        type: string
        enum:
          - 'machine-cidr-defined'
          ...
          - 'lso-requirements-satisfied'
          - 'cnv-requirements-satisfied'
          - 'odf-requirements-satisfied'
          - 'lvm-requirements-satisfied'
      ```
 2. Introduce new feature support ID in the [swagger specification](../../swagger.yaml):
  ```yaml
  feature-support-level-id:
    type: string
    enum:
      - 'SNO'
      ...
      - 'LVM'
      - 'ODF'
      - 'LSO'
      - 'CNV'
  ```
 3. Add the operator's name to the enum list for `/v2/supported-operators` endpoint [swagger specification](../../swagger.yaml):
  ```yaml
    type: array
    items:
      type: string
      enum:
      - 'amd-gpu'
      - 'lso'
      - 'mtv'
      - 'openshift-ai'
      ...
      - 'osc'
      - 'servicemesh'
  ```
 4. Regenerate code by running
  ```shell script
  skipper make generate
  ```
 5. Create and add the new validation IDs to proper category - "operators":
    - for [cluster validation](../../internal/cluster/validation_id.go):
      ```go
      func (v validationID) category() (string, error) {
      ...
        case IsCnvRequirementsSatisfied, IsLsoRequirementsSatisfied, IsOdfRequirementsSatisfied, IsLvmRequirementsSatisfied:
     	   return "operators", nil
      ```
    - for [host validaton](../../internal/host/validation_id.go):
      ```go
      func (v validationID) category() (string, error) {
      ...
        case AreLsoRequirementsSatisfied, AreCnvRequirementsSatisfied, AreOdfRequirementsSatisfied, AreLvmRequirementsSatisfied:
      		return "operators", nil
      ```
 6. Modify the installation state machine by adding the new validationIDs to the list of required checks:
    - for [cluster](../../internal/cluster/statemachine.go):
      ```go
      var requiredForInstall = stateswitch.And(...,
         ..., If(IsLsoRequirementsSatisfied), If(IsCnvRequirementsSatisfied), If(IsOdfRequirementsSatisfied), If(IsLvmRequirementsSatisfied))
      ```
    - for [host](../../internal/host/statemachine.go):
      ```go
      	var isSufficientForInstall = stateswitch.And(...,
      		...,
      		If(AreLsoRequirementsSatisfied), If(AreCnvRequirementsSatisfied), If(AreOdfRequirementsSatisfied), If(AreLvmRequirementsSatisfied))
      ```
 7. Add the new feature to the OLM operators list and implement the [SupportLevelFeature interface](../../internal/featuresupport/support_level_feature.go)
    - add feature to the [support level list](../../internal/featuresupport/feature_support_level.go):
      ```go
	      // Olm Operators features
	      models.FeatureSupportLevelIDLVM:                  (&LvmFeature{}).New(),
        ...
	      models.FeatureSupportLevelIDMCE:                  (&MceFeature{}).New(),
	      models.FeatureSupportLevelIDODF:                  (&OdfFeature{}).New(),
	      models.FeatureSupportLevelIDMTV:                  (&MtvFeature{}).New(),
	      models.FeatureSupportLevelIDOSC:                  (&OscFeature{}).New(),
      ```
    - implement the interface at the end of [olm operators](../../internal/featuresupport/features_olm_operators.go) 
      Your custom code shall reflect the requirements of this new feature by means of Openshift version and target platform (`getSupportLevel`), CPU architecture (`getIncompatibleArchitectures`), and compatibility with existing features (`getIncompatibleFeatures`).
    - in case of any such incompatibilities, add the feature also to the `getIncompatibleFeatures` methods of all the affected types:
      - [for misc features](../../internal/featuresupport/features_misc.go)
      - [for networking features](../../internal/featuresupport/features_pnetworking.go)
      - [for other existing operators](../../internal/featuresupport/features_olm_operators.go)
      - [for platforms](../../internal/featuresupport/features_platforms.go)

 8. Implement the [Operator interface](../../internal/operators/api/api.go)
 9. Plug the new `Operator` implementation in the [OperatorManager constructor](../../internal/operators/builder.go):
  ```go
  func NewManager(log logrus.FieldLogger) Manager {
    return NewManagerWithOperators(log, lso.NewLSOperator(), cnv.NewCnvOperator(log), odf.NewOdfOperator(log), lvm.NewLvmOperator(log))
  }
  ```

## Notes about the Operator interface

### Manifests generation
A plugin can generate two distinct set of manifests, specified by the two return values of the `GenerateManifests(*common.Cluster) (map[string][]byte, []byte, error)` method. They are required to:

1. Install the operator
2. Configure the operator

The first return value could be used to specify a set of manifests that will be applied directly to the control plane during the bootstrap phase. This set is usually composed by a 
manifest for creating a new namespace, a new subscription and a new operator group CR for the involved operator.

The second return value it's a manifest used to configure the freshly installed operator, and it will be applied by the ```assisted-installer-controller``` job, only after the cluster have been successfully created and the OLM operators are all ready (currently the ```assisted-installer-controller``` retrieves the whole list of configurations by downloading the ```custom_manifests.json``` file fetched from the Assisted Service).
