package ocs

import (
	"context"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

// ocsOperator is an OCS OLM operator plugin; it implements api.Operator
type ocsOperator struct {
	log                logrus.FieldLogger
	ocsValidatorConfig Config
	ocsValidator       OCSValidator
}

var Operator models.MonitoredOperator = models.MonitoredOperator{
	Name:           "ocs",
	OperatorType:   models.OperatorTypeOlm,
	TimeoutSeconds: 30 * 60,
}

// NewOcsOperator creates new OCSOperator
func NewOcsOperator(log logrus.FieldLogger) *ocsOperator {
	cfg := Config{}
	err := envconfig.Process("myapp", &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	validator := NewOCSValidator(log.WithField("pkg", "ocs-operator-state"), &cfg)
	return NewOcsOperatorWithConfig(log, cfg, validator)
}

// NewOcsOperatorWithConfig creates new OCSOperator with given configuration and validator
func NewOcsOperatorWithConfig(log logrus.FieldLogger, config Config, validator OCSValidator) *ocsOperator {
	return &ocsOperator{
		log:                log,
		ocsValidatorConfig: config,
		ocsValidator:       validator,
	}
}

// GetName reports the name of an operator this Operator manages
func (o *ocsOperator) GetName() string {
	return Operator.Name
}

// GetDependencies provides a list of dependencies of the Operator
func (o *ocsOperator) GetDependencies() []string {
	return []string{lso.Operator.Name}
}

// GetClusterValidationID returns cluster validation ID for the Operator
func (o *ocsOperator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDOcsRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the Operator
func (o *ocsOperator) GetHostValidationID() string {
	return string(models.HostValidationIDOcsRequirementsSatisfied)
}

// ValidateCluster verifies whether this operator is valid for given cluster
func (o *ocsOperator) ValidateCluster(_ context.Context, cluster *common.Cluster) (api.ValidationResult, error) {
	status, message := o.ocsValidator.ValidateRequirements(&cluster.Cluster)

	return api.ValidationResult{Status: status, ValidationId: o.GetClusterValidationID(), Reasons: []string{message}}, nil
}

// ValidateHost verifies whether this operator is valid for given host
func (o *ocsOperator) ValidateHost(context.Context, *common.Cluster, *models.Host) (api.ValidationResult, error) {
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID(), Reasons: []string{}}, nil
}

// GenerateManifests generates manifests for the operator
func (o *ocsOperator) GenerateManifests(cluster *common.Cluster) (*api.Manifests, error) {
	manifests, err := Manifests(o.ocsValidatorConfig.OCSMinimalDeployment, o.ocsValidatorConfig.OCSDisksAvailable, len(cluster.Cluster.Hosts))
	return &api.Manifests{Files: manifests}, err
}

// GetCPURequirementForWorker provides worker CPU requirements for the operator
func (o *ocsOperator) GetCPURequirementForWorker(context.Context, *common.Cluster) (int64, error) {
	return 0, nil
}

// GetCPURequirementForMaster provides master CPU requirements for the operator
func (o *ocsOperator) GetCPURequirementForMaster(context.Context, *common.Cluster) (int64, error) {
	return 0, nil
}

// GetMemoryRequirementForWorker provides worker memory requirements for the operator in MB
func (o *ocsOperator) GetMemoryRequirementForWorker(context.Context, *common.Cluster) (int64, error) {
	return 0, nil
}

// GetMemoryRequirementForMaster provides master memory requirements for the operator
func (o *ocsOperator) GetMemoryRequirementForMaster(context.Context, *common.Cluster) (int64, error) {
	return 0, nil
}

// GetDisksRequirementForMaster provides a number of disks required in a master
func (o *ocsOperator) GetDisksRequirementForMaster(context.Context, *common.Cluster) (int64, error) {
	return 0, nil
}

// GetDisksRequirementForWorker provides a number of disks required in a worker
func (o *ocsOperator) GetDisksRequirementForWorker(context.Context, *common.Cluster) (int64, error) {
	return 0, nil
}

// GetProperties provides description of operator properties: none required
func (o *ocsOperator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}
