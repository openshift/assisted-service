package lso

import (
	"context"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
)

// lsOperator is an LSO OLM operator plugin; it implements api.Operator
type lsOperator struct {
}

var Operator = models.MonitoredOperator{
	Name:             "lso",
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        "openshift-local-storage",
	SubscriptionName: "local-storage-operator",
	TimeoutSeconds:   70 * 60,
}

// New LSOperator creates new instance of a Local Storage Operator installation plugin
func NewLSOperator() *lsOperator {
	return &lsOperator{}
}

// GetName reports the name of an operator this Operator manages
func (l *lsOperator) GetName() string {
	return Operator.Name
}

func (l *lsOperator) GetFullName() string {
	return "Local Storage Operator"
}

// GetDependencies provides a list of dependencies of the Operator
func (l *lsOperator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	return make([]string, 0), nil
}

// GetClusterValidationID returns cluster validation ID for the Operator
func (l *lsOperator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDLsoRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the Operator
func (l *lsOperator) GetHostValidationID() string {
	return string(models.HostValidationIDLsoRequirementsSatisfied)
}

// ValidateCluster always return "valid" result
func (l *lsOperator) ValidateCluster(_ context.Context, _ *common.Cluster) (api.ValidationResult, error) {
	return api.ValidationResult{Status: api.Success, ValidationId: l.GetClusterValidationID(), Reasons: []string{}}, nil
}

// ValidateHost always return "valid" result
func (l *lsOperator) ValidateHost(_ context.Context, _ *common.Cluster, _ *models.Host) (api.ValidationResult, error) {
	return api.ValidationResult{Status: api.Success, ValidationId: l.GetHostValidationID(), Reasons: []string{}}, nil
}

// GenerateManifests generates manifests for the operator

func (l *lsOperator) GenerateManifests(c *common.Cluster) (map[string][]byte, []byte, error) {
	return Manifests()
}

// GetProperties provides description of operator properties: none required
func (l *lsOperator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to the LSO
func (l *lsOperator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GetHostRequirements provides operator's requirements towards the host
func (l *lsOperator) GetHostRequirements(context.Context, *common.Cluster, *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	return &models.ClusterHostRequirementsDetails{}, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (l *lsOperator) GetPreflightRequirements(context context.Context, cluster *common.Cluster) (*models.OperatorHardwareRequirements, error) {
	dependecies, err := l.GetDependencies(cluster)
	if err != nil {
		return &models.OperatorHardwareRequirements{}, err
	}
	return &models.OperatorHardwareRequirements{
		OperatorName: l.GetName(),
		Dependencies: dependecies,
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{},
			},
		},
	}, nil
}

func (l *lsOperator) GetSupportedArchitectures() []string {
	return []string{common.X86CPUArchitecture}
}
