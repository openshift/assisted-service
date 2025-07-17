package nodefeaturediscovery

import (
	"context"
	"text/template"

	"github.com/kelseyhightower/envconfig"
	"github.com/lib/pq"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/templating"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

const (
	clusterValidationID = string(models.ClusterValidationIDNodeFeatureDiscoveryRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDNodeFeatureDiscoveryRequirementsSatisfied)
)

var Operator = models.MonitoredOperator{
	Namespace:        "openshift-nfd",
	Name:             "node-feature-discovery",
	OperatorType:     models.OperatorTypeOlm,
	SubscriptionName: "nfd",
	TimeoutSeconds:   30 * 60,
	Bundles: pq.StringArray{
		operatorscommon.BundleOpenShiftAI.ID,
	},
}

// operator is an node feature discovery OLM operator plugin.
type operator struct {
	log       logrus.FieldLogger
	config    *Config
	templates *template.Template
}

// NewFeatureDiscoveryOperator creates new node feature discovery operator.
func NewNodeFeatureDiscoveryOperator(log logrus.FieldLogger) *operator {
	config := &Config{}
	err := envconfig.Process(common.EnvConfigPrefix, config)
	if err != nil {
		log.Fatal(err.Error())
	}
	templates, err := templating.LoadTemplates(templatesRoot)
	if err != nil {
		log.Fatal(err.Error())
	}
	return &operator{
		log:       log,
		config:    config,
		templates: templates,
	}
}

// GetName reports the name of an operator.
func (o *operator) GetName() string {
	return Operator.Name
}

func (o *operator) GetFullName() string {
	return "Node Feature Discovery"
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(c *common.Cluster) ([]string, error) {
	return make([]string, 0), nil
}

func (o *operator) GetDependenciesFeatureSupportID() []models.FeatureSupportLevelID {
	return nil
}

// GetClusterValidationIDs returns cluster validation IDs for the operator.
func (o *operator) GetClusterValidationIDs() []string {
	return []string{clusterValidationID}
}

// GetHostValidationID returns host validation ID for the operator.
func (o *operator) GetHostValidationID() string {
	return hostValidationID
}

// ValidateCluster checks if the cluster satisfies the requirements to install the operator.
func (o *operator) ValidateCluster(ctx context.Context, cluster *common.Cluster) ([]api.ValidationResult, error) {
	return []api.ValidationResult{{
		ValidationId: clusterValidationID,
		Status:       api.Success,
	}}, nil
}

// ValidateHost returns validationResult based on node type requirements such as memory and CPU.
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host,
	hostRequirements *models.ClusterHostRequirementsDetails) (result api.ValidationResult, err error) {
	result = api.ValidationResult{
		Status:       api.Success,
		ValidationId: o.GetHostValidationID(),
	}
	return
}

// GetProperties provides description of operator properties.
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns the information that describes how to monitor the operator.
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GetHostRequirements provides the requirements that the host needs to satisfy in order to be able to install the
// operator.
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster,
	host *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	preflightRequirements, err := o.GetPreflightRequirements(ctx, cluster)
	if err != nil {
		o.log.WithError(err).Errorf("Cannot retrieve preflight requirements for cluster %s", cluster.ID)
		return nil, err
	}
	return preflightRequirements.Requirements.Worker.Quantitative, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only.
func (o *operator) GetPreflightRequirements(context context.Context,
	cluster *common.Cluster) (result *models.OperatorHardwareRequirements, err error) {
	dependencies, err := o.GetDependencies(cluster)
	if err != nil {
		return
	}
	result = &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: dependencies,
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{},
			},
		},
	}
	return
}

func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNODEFEATUREDISCOVERY
}

func (o *operator) GetBundleLabels() []string {
	return []string(Operator.Bundles)
}
