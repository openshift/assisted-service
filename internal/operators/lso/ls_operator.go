package lso

import (
	"context"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
)

type lsOperator struct {
}

// New LsOperator creates new instance of a Local Storage Operator installation plugin
func NewLSOperator() *lsOperator {
	return &lsOperator{}
}

// GetType reports type of an operator this Operator manages
func (l *lsOperator) GetType() models.OperatorType {
	return models.OperatorTypeLso
}

// GetDependencies provides a list of dependencies of the Operator
func (l *lsOperator) GetDependencies() []models.OperatorType {
	return make([]models.OperatorType, 0)
}

func (l *lsOperator) GetValidationID() models.ClusterValidationID {
	return models.ClusterValidationIDLsoRequirementsSatisfied
}

// ValidateCluster always return "valid" result
func (l *lsOperator) ValidateCluster(ctx context.Context, cluster *common.Cluster) (api.ValidationResult, error) {
	return api.ValidationResult{Valid: true, ValidationId: l.GetValidationID()}, nil
}

// ValidateHost always return "valid" result
func (l *lsOperator) ValidateHost(context.Context, *common.Cluster, *models.Host) (api.ValidationResult, error) {
	return api.ValidationResult{Valid: true}, nil
}

// GetCPURequirementForWorker provides worker CPU requirements for the operator
func (l *lsOperator) GetCPURequirementForWorker(context.Context, *common.Cluster) (int64, error) {
	return 0, nil
}

// GetCPURequirementForMaster provides master CPU requirements for the operator
func (l *lsOperator) GetCPURequirementForMaster(context.Context, *common.Cluster) (int64, error) {
	return 0, nil
}

// GetMemoryRequirementForWorker provides worker memory requirements for the operator
func (l *lsOperator) GetMemoryRequirementForWorker(ctx context.Context, cluster *common.Cluster) (int64, error) {
	return 0, nil
}

// GetMemoryRequirementForMaster provides master memory requirements for the operator
func (l *lsOperator) GetMemoryRequirementForMaster(ctx context.Context, cluster *common.Cluster) (int64, error) {
	return 0, nil
}

// GenerateManifests generates manifests for the operator
func (l *lsOperator) GenerateManifests(c *common.Cluster) (*api.Manifests, error) {
	manifestFiles, err := Manifests(c.Cluster.OpenshiftVersion)
	return &api.Manifests{Files: manifestFiles}, err

}
