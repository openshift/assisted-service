package authorino

import (
	"context"
	"text/template"

	"github.com/lib/pq"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/templating"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var Operator = models.MonitoredOperator{
	Namespace:        "openshift-operators",
	Name:             "authorino",
	OperatorType:     models.OperatorTypeOlm,
	SubscriptionName: "authorino-operator",
	TimeoutSeconds:   30 * 60,
	Bundles: pq.StringArray{
		operatorscommon.BundleOpenShiftAI.ID,
	},
}

// operator is an Authorino AI OLM operator plugin.
type operator struct {
	log       logrus.FieldLogger
	templates *template.Template
}

// NewAuthorinoIOperator creates new Authorino operator.
func NewAuthorinoOperator(log logrus.FieldLogger) *operator {
	templates, err := templating.LoadTemplates(templatesRoot)
	if err != nil {
		log.Fatal(err.Error())
	}
	return &operator{
		log:       log,
		templates: templates,
	}
}

// GetName reports the name of an operator.
func (o *operator) GetName() string {
	return Operator.Name
}

func (o *operator) GetFullName() string {
	return "Authorino"
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(c *common.Cluster) (result []string, err error) {
	return
}

// GetClusterValidationID returns cluster validation ID for the operator.
func (o *operator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDAuthorinoRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the operator.
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDAuthorinoRequirementsSatisfied)
}

// ValidateCluster checks if the cluster satisfies the requirements to install the operator.
func (o *operator) ValidateCluster(ctx context.Context, cluster *common.Cluster) (result api.ValidationResult,
	err error) {
	result.ValidationId = o.GetClusterValidationID()
	result.Status = api.Success
	return
}

// ValidateHost returns validationResult based on node type requirements such as memory and CPU.
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host,
	_ *models.ClusterHostRequirementsDetails) (result api.ValidationResult, err error) {
	result.ValidationId = o.GetHostValidationID()
	result.Status = api.Success
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
	host *models.Host) (result *models.ClusterHostRequirementsDetails, err error) {
	preflightRequirements, err := o.GetPreflightRequirements(ctx, cluster)
	if err != nil {
		return
	}
	result = preflightRequirements.Requirements.Worker.Quantitative
	return
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
	return models.FeatureSupportLevelIDAUTHORINO
}

// GetBundleLabels returns the bundle labels for the Authorino operator
func (o *operator) GetBundleLabels() []string {
	return []string(Operator.Bundles)
}
