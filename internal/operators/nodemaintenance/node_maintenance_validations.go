package nodemaintenance

import (
	"context"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
)

// ValidateCluster checks if the cluster satisfies the requirements to install the operator.
func (o *operator) ValidateCluster(ctx context.Context, cluster *common.Cluster) (api.ValidationResult, error) {
	return api.ValidationResult{
		Status:       api.Success,
		ValidationId: o.GetClusterValidationID(),
	}, nil
}

// ValidateHost returns validationResult based on node type requirements such as memory and CPU.
func (o *operator) ValidateHost(
	ctx context.Context,
	cluster *common.Cluster,
	host *models.Host,
	hostRequirements *models.ClusterHostRequirementsDetails,
) (api.ValidationResult, error) {
	return api.ValidationResult{
		Status:       api.Success,
		ValidationId: o.GetHostValidationID(),
	}, nil
}
