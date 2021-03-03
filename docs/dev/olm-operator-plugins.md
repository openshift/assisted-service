# OLM operator plugins development

## Existing plugins
  - [Local Storage Operator (LSO)](../../internal/operators/lso)
  - [OpenShift Container Storage (OCS)](../../internal/operators/ocs)

## How to implement a new OLM operator plugin

To implement support for a new OLM operator plugin you need to make following changes:

 1. Introduce a new OLM operator type in the [swagger specification](../../swagger.yaml):
    - by adding the new value in the `operator-type` definition:
      ```yaml
       operator-type:
       type: string
       enum:
         - 'lso'
         - 'ocs'
       ``` 
    - by adding the new value in the `ListOperatorProperties` operation `operator_type` parameter enum:
      ```yaml
      operationId: ListOperatorProperties
      parameters:
        - in: path
          name: operator_type
          description: The operator type.
          type: string
          enum: ['lso', 'ocs']
          required: true
      ```                      
 1. Introduce new validation IDs for the new operator in the [swagger specification](../../swagger.yaml):
    - for host validation:
      ```yaml
      host-validation-id:
        type: string
        enum:
          - 'connected'
          ...
          - 'lso-requirements-satisfied'
          - 'ocs-requirements-satisfied' 
      ```                   
    - for cluster validation:    
      ```yaml
      cluster-validation-id:
        type: string
        enum:
          - 'machine-cidr-defined'
          ...
          - 'lso-requirements-satisfied'
          - 'ocs-requirements-satisfied'
      ```
 1. Regenerate code by running
    ```shell script
    skipper make generate-all 
    ```      
 1. Add the new validation IDs to proper category - "operators":
    - for [cluster validation](../../internal/cluster/validation_id.go):
      ```go
      func (v validationID) category() (string, error) {
      ...
        case IsOcsRequirementsSatisfied, IsLsoRequirementsSatisfied:
     	   return "operators", nil
      ``` 
    - for [host validaton](../../internal/host/validation_id.go):
      ```go
      func (v validationID) category() (string, error) {
      ...
        case AreLsoRequirementsSatisfied, AreOcsRequirementsSatisfied:
      		return "operators", nil
      ```
 1. Modify the installation state machine by adding the new validationIDs to the list of required checks:
    - for [cluster](../../internal/cluster/statemachine.go):
      ```go 
      var requiredForInstall = stateswitch.And(...,
         ..., If(IsOcsRequirementsSatisfied), If(IsLsoRequirementsSatisfied))
      ```     
    - for [host](../../internal/host/statemachine.go):
      ```go
      	var isSufficientForInstall = stateswitch.And(...,
      		...,
      		If(AreOcsRequirementsSatisfied), If(AreLsoRequirementsSatisfied))
      ```
 1. Implement the [`Operator` interface](../../internal/operators/api/api.go)
 1. Plug the new `Operator` implementation in the [OperatorManager constructor](../../internal/operators/builder.go):
    ```go
    func NewManager(log logrus.FieldLogger) Manager {
    	return NewManagerWithOperators(log, lso.NewLSOperator(), ocs.NewOcsOperator(log))
    }
    ```
 1. Implement tests verifying new OLM operator installation and validation, i.e. in [internal/bminventory/inventory_test.go](../../internal/bminventory/inventory_test.go)
 1. Make sure all the tests are green