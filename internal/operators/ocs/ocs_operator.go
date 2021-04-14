package ocs

import (
	"context"
	"fmt"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

// operator is an OCS OLM operator plugin; it implements api.Operator
type operator struct {
	log    logrus.FieldLogger
	config *Config
}

var Operator = models.MonitoredOperator{
	Name:             "ocs",
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        "openshift-storage",
	SubscriptionName: "ocs-operator",
	TimeoutSeconds:   30 * 60,
}

// NewOcsOperator creates new OCSOperator
func NewOcsOperator(log logrus.FieldLogger) *operator {
	cfg := Config{}
	err := envconfig.Process(common.EnvConfigPrefix, &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return newOcsOperatorWithConfig(log, &cfg)
}

// newOcsOperatorWithConfig creates new OCSOperator with given configuration
func newOcsOperatorWithConfig(log logrus.FieldLogger, config *Config) *operator {
	return &operator{
		log:    log,
		config: config,
	}
}

// GetName reports the name of an operator this Operator manages
func (o *operator) GetName() string {
	return Operator.Name
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies() []string {
	return []string{lso.Operator.Name}
}

// GetClusterValidationID returns cluster validation ID for the Operator
func (o *operator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDOcsRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDOcsRequirementsSatisfied)
}

// ValidateCluster verifies whether this operator is valid for given cluster
func (o *operator) ValidateCluster(_ context.Context, cluster *common.Cluster) (api.ValidationResult, error) {
	status, message := o.validateRequirements(&cluster.Cluster)

	return api.ValidationResult{Status: status, ValidationId: o.GetClusterValidationID(), Reasons: []string{message}}, nil
}

// ValidateHost verifies whether this operator is valid for given host
func (o *operator) ValidateHost(_ context.Context, _ *common.Cluster, _ *models.Host) (api.ValidationResult, error) {
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID(), Reasons: []string{}}, nil
}

// GenerateManifests generates manifests for the operator
func (o *operator) GenerateManifests(cluster *common.Cluster) (map[string][]byte, error) {
	o.log.Info("No. of OCS eligible disks are ", o.config.OCSDisksAvailable)
	return Manifests(o.config)
}

// GetProperties provides description of operator properties: none required
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to the OCS Operator
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GetHostRequirements provides operator's requirements towards the host
func (o *operator) GetHostRequirements(context.Context, *common.Cluster, *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	return &models.ClusterHostRequirementsDetails{}, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(context.Context, *common.Cluster) (*models.OperatorHardwareRequirements, error) {
	return &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: o.GetDependencies(),
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				// TODO: adjust when https://github.com/openshift/assisted-service/pull/1456 is merged
				Quantitative: &models.ClusterHostRequirementsDetails{},
			},
			Worker: &models.HostTypeHardwareRequirements{
				// TODO: adjust when https://github.com/openshift/assisted-service/pull/1456 is merged
				Quantitative: &models.ClusterHostRequirementsDetails{},
				Qualitative: []string{
					fmt.Sprintf("%v GiB of additional RAM for each non-boot disk", o.config.OCSRequiredDiskRAMGB),
					fmt.Sprintf("%v additional CPUs for each non-boot disk", o.config.OCSRequiredDiskCPUCount),
				},
			},
		},
	}, nil
}
