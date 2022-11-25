package mce

import (
	"context"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

// operator is an MCE OLM operator plugin.
type operator struct {
	log    logrus.FieldLogger
	config *Config
}

var monitoredOperator = models.MonitoredOperator{
	Name:             "mce",
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        "mce",
	SubscriptionName: "mce",
	TimeoutSeconds:   30 * 60,
}

// NewMceOperator creates new MCE operator.
func NewMceOperator(log logrus.FieldLogger) *operator {
	cfg := Config{}
	err := envconfig.Process(common.EnvConfigPrefix, &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return newMceOperatorWithConfig(log, &cfg)
}

// newMceOperatorWithConfig creates new MCE operator with the given configuration.
func newMceOperatorWithConfig(log logrus.FieldLogger, config *Config) *operator {
	return &operator{
		log:    log,
		config: config,
	}
}

// GetName reports the name of an operator.
func (o *operator) GetName() string {
	return monitoredOperator.Name
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	return make([]string, 0), nil
}

// GetClusterValidationID returns cluster validation ID for the operator.
func (o *operator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDMceRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the operator.
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDMceRequirementsSatisfied)
}

// ValidateCluster checks if the cluster satisfies the requirements to install the operator.
func (o *operator) ValidateCluster(_ context.Context,
	cluster *common.Cluster) (result api.ValidationResult, err error) {
	result = api.ValidationResult{
		Status:       api.Success,
		ValidationId: o.GetClusterValidationID(),
	}
	return
}

// ValidateHost checks if the host satisfies the requirements to install the operator.
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster,
	host *models.Host) (result api.ValidationResult, err error) {
	result = api.ValidationResult{
		Status:       api.Success,
		ValidationId: o.GetHostValidationID(),
	}
	return
}

// GenerateManifests generates manifests for the operator.
func (o *operator) GenerateManifests(_ *common.Cluster) (map[string][]byte, []byte, error) {
	return Manifests()
}

// GetProperties provides description of operator properties.
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns the information that describes how to monitor the operator.
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &monitoredOperator
}

// GetHostRequirements provides the requirements that the host needs to satisfy in order to be able
// to install the operator.
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster,
	_ *models.Host) (result *models.ClusterHostRequirementsDetails, err error) {
	log := logutil.FromContext(ctx, o.log)
	preflightRequirements, err := o.GetPreflightRequirements(ctx, cluster)
	if err != nil {
		log.WithError(err).Errorf("Cannot retrieve preflight requirements for cluster %s", cluster.ID)
		return nil, err
	}
	return preflightRequirements.Requirements.Master.Quantitative, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with
// cluster data only.
func (o *operator) GetPreflightRequirements(context context.Context,
	cluster *common.Cluster) (result *models.OperatorHardwareRequirements, err error) {
	dependecies, err := o.GetDependencies(cluster)
	if err != nil {
		return &models.OperatorHardwareRequirements{}, err
	}
	result = &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: dependecies,
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Qualitative:  []string{},
				Quantitative: &models.ClusterHostRequirementsDetails{},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Qualitative:  []string{},
				Quantitative: &models.ClusterHostRequirementsDetails{},
			},
		},
	}
	return
}
