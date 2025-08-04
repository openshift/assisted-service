package metallb

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

const (
	clusterValidationID = string(models.ClusterValidationIDMetallbRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDMetallbRequirementsSatisfied)
)

// operator is a MetalLB OLM operator plugin; it implements api.Operator
type operator struct {
	log       logrus.FieldLogger
	templates *template.Template
}

var Operator = models.MonitoredOperator{
	Name:             "metallb",
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        operatorNamespace,
	SubscriptionName: operatorSubscriptionName,
	TimeoutSeconds:   30 * 60,
	Bundles: pq.StringArray{
		operatorscommon.BundleVirtualization.ID,
	},
}

// NewMetalLBOperator creates new MetalLB operator
func NewMetalLBOperator(log logrus.FieldLogger) *operator {
	return newMetalLBOperatorWithConfig(log)
}

// newMetalLBOperatorWithConfig creates new MetalLB operator with given configuration
func newMetalLBOperatorWithConfig(log logrus.FieldLogger) *operator {
	templates, err := templating.LoadTemplates(templatesRoot)
	if err != nil {
		log.Fatal(err.Error())
	}
	return &operator{
		log:       log,
		templates: templates,
	}
}

// GetName reports the name of an operator this Operator manages
func (o *operator) GetName() string {
	return Operator.Name
}

func (o *operator) GetFullName() string {
	return "MetalLB"
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	return make([]string, 0), nil
}

// GetDependenciesFeatureSupportID returns feature support level IDs for the Operator
func (o *operator) GetDependenciesFeatureSupportID() []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDMETALLB,
	}
}

// GetClusterValidationIDs returns cluster validation IDs for the Operator
func (o *operator) GetClusterValidationIDs() []string {
	return []string{clusterValidationID}
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return hostValidationID
}

// ValidateCluster validates cluster requirements for MetalLB
func (o *operator) ValidateCluster(_ context.Context, cluster *common.Cluster) ([]api.ValidationResult, error) {
	result := []api.ValidationResult{{
		Status:       api.Success,
		ValidationId: clusterValidationID,
	}}

	return result, nil
}

// ValidateHost validates host requirements for MetalLB
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host, additionalOperatorRequirements *models.ClusterHostRequirementsDetails) (api.ValidationResult, error) {
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID()}, nil
}

// GetProperties provides description of operator properties: none required
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to MetalLB
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GetHostRequirements provides operator's requirements towards the host
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	// MetalLB has minimal resource requirements
	return &models.ClusterHostRequirementsDetails{}, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(ctx context.Context, cluster *common.Cluster) (*models.OperatorHardwareRequirements, error) {
	dependencies, err := o.GetDependencies(cluster)
	if err != nil {
		return &models.OperatorHardwareRequirements{}, err
	}

	return &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: dependencies,
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
	}, nil
}

// GetFeatureSupportID returns the feature support level ID for MetalLB
func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDMETALLB
}

// GetBundleLabels returns bundle labels for MetalLB
func (o *operator) GetBundleLabels() []string {
	return []string{}
}
