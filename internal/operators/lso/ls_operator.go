package lso

import (
	"context"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
)

const (
	clusterValidationID = string(models.ClusterValidationIDLsoRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDLsoRequirementsSatisfied)
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

func (o *lsOperator) GetDependenciesFeatureSupportID() []models.FeatureSupportLevelID {
	return nil
}

// GetClusterValidationIDs returns cluster validation IDs for the Operator
func (l *lsOperator) GetClusterValidationIDs() []string {
	return []string{clusterValidationID}
}

// GetHostValidationID returns host validation ID for the Operator
func (l *lsOperator) GetHostValidationID() string {
	return hostValidationID
}

// ValidateCluster always return "valid" result
func (l *lsOperator) ValidateCluster(_ context.Context, _ *common.Cluster) ([]api.ValidationResult, error) {
	return []api.ValidationResult{{
		Status:       api.Success,
		ValidationId: clusterValidationID,
	}}, nil
}

// ValidateHost always return "valid" result
func (l *lsOperator) ValidateHost(_ context.Context, _ *common.Cluster, _ *models.Host, _ *models.ClusterHostRequirementsDetails) (api.ValidationResult, error) {
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

func (l *lsOperator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDLSO
}

// GetBundleLabels returns the bundle labels for the LSO operator
func (l *lsOperator) GetBundleLabels() []string {
	return []string(Operator.Bundles)
}
