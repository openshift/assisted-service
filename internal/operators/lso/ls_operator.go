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

// GetDependencies provides a list of dependencies of the Operator
func (l *lsOperator) GetDependencies() []string {
	return make([]string, 0)
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
func (l *lsOperator) GenerateManifests(c *common.Cluster) (map[string][]byte, error) {
	return Manifests(c.Cluster.OpenshiftVersion)
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
