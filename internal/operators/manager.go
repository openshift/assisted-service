package operators

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

// Manager is responsible for performing operations against additional operators
type Manager struct {
	log       logrus.FieldLogger
	operators map[models.OperatorType]api.Operator
}

//go:generate  mockgen -package=operators -destination=mock_operators_api.go . API
type API interface {
	// ValidateCluster validates cluster requirements
	ValidateCluster(ctx context.Context, cluster *common.Cluster) ([]api.ValidationResult, error)
	// ValidateHost validates host requirements
	ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host) ([]api.ValidationResult, error)
	// GenerateManifests generates manifests for all enabled operators.
	// Returns map assigning manifest content to its desired file name
	GenerateManifests(cluster *common.Cluster) (map[string]string, error)
	// AnyOperatorEnabled checks whether any operator has been enabled for the given cluster
	AnyOperatorEnabled(cluster *common.Cluster) bool
	// UpdateDependencies amends the list of requested additional operators with any missing dependencies
	UpdateDependencies(cluster *common.Cluster) error
}

// GenerateManifests generates manifests for all enabled operators.
// Returns map assigning manifest content to its desired file name
func (mgr *Manager) GenerateManifests(cluster *common.Cluster) (map[string]string, error) {
	// TODO: cluster should already contain up-to-date list of operators - implemented here for now to replicate
	// the original behaviour
	err := mgr.UpdateDependencies(cluster)
	if err != nil {
		return nil, err
	}

	operatorManifests := make(map[string]string)

	clusterOperators, err := unmarshallOperators(cluster.Operators)
	if err != nil {
		return nil, err
	}
	// Generate manifests for all the generic operators
	for _, clusterOperator := range clusterOperators {
		operator := mgr.operators[clusterOperator.OperatorType]
		if swag.BoolValue(clusterOperator.Enabled) && operator != nil {
			manifests, err := operator.GenerateManifests(cluster)
			if err != nil {
				mgr.log.Error(fmt.Sprintf("Cannot generate %v manifests due to ", clusterOperator.OperatorType), err)
				return nil, err
			}
			if manifests != nil {
				for k, v := range manifests.Files {
					operatorManifests[k] = v
				}
			}
		}
	}

	return operatorManifests, nil
}

// AnyOperatorEnabled checks whether any operator has been enabled for the given cluster
func (mgr *Manager) AnyOperatorEnabled(cluster *common.Cluster) bool {
	for _, operator := range mgr.operators {
		if isEnabled(cluster, operator.GetType()) {
			return true
		}
	}
	return false
}

// ValidateHost validates host requirements
func (mgr *Manager) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host) ([]api.ValidationResult, error) {
	// TODO: cluster should already contain up-to-date list of operators - implemented here for now to replicate
	// the original behaviour
	err := mgr.UpdateDependencies(cluster)
	if err != nil {
		return nil, err
	}
	clusterOperators, err := unmarshallOperators(cluster.Operators)
	if err != nil {
		return nil, err
	}

	results := make([]api.ValidationResult, 0, len(mgr.operators))

	// To track operators that are disabled or not present in the cluster configuration, but have to be present
	// in the validation results and marked as valid.
	pendingOperators := make(map[models.OperatorType]bool)
	for k := range mgr.operators {
		pendingOperators[k] = true
	}

	for _, clusterOperator := range clusterOperators {
		operator := mgr.operators[clusterOperator.OperatorType]
		var result api.ValidationResult
		if operator != nil {
			if swag.BoolValue(clusterOperator.Enabled) {
				result, err = operator.ValidateHost(ctx, cluster, host)
				if err != nil {
					return nil, err
				}
				delete(pendingOperators, clusterOperator.OperatorType)
				results = append(results, result)
			}
		}
	}
	// Add successful validation result for disabled operators
	for opType := range pendingOperators {
		operator := mgr.operators[opType]
		result := api.ValidationResult{
			Status:       api.Success,
			ValidationId: operator.GetHostValidationID(),
			Reasons: []string{
				fmt.Sprintf("%v is disabled", opType),
			},
		}
		results = append(results, result)
	}
	return results, nil
}

// ValidateCluster validates cluster requirements
func (mgr *Manager) ValidateCluster(ctx context.Context, cluster *common.Cluster) ([]api.ValidationResult, error) {
	// TODO: cluster should already contain up-to-date list of operators - implemented here for now to replicate
	// the original behaviour
	err := mgr.UpdateDependencies(cluster)
	if err != nil {
		return nil, err
	}
	clusterOperators, err := unmarshallOperators(cluster.Operators)
	if err != nil {
		return nil, err
	}

	results := make([]api.ValidationResult, 0, len(mgr.operators))

	pendingOperators := make(map[models.OperatorType]bool)
	for k := range mgr.operators {
		pendingOperators[k] = true
	}

	for _, clusterOperator := range clusterOperators {
		operator := mgr.operators[clusterOperator.OperatorType]
		var result api.ValidationResult
		if operator != nil {
			if swag.BoolValue(clusterOperator.Enabled) {
				result, err = operator.ValidateCluster(ctx, cluster)
				if err != nil {
					return nil, err
				}
				delete(pendingOperators, clusterOperator.OperatorType)
				results = append(results, result)
			}
		}
	}
	// Add successful validation result for disabled operators
	for opType := range pendingOperators {
		operator := mgr.operators[opType]
		result := api.ValidationResult{
			Status:       api.Success,
			ValidationId: operator.GetClusterValidationID(),
			Reasons: []string{
				fmt.Sprintf("%v is disabled", opType),
			},
		}
		results = append(results, result)
	}
	return results, nil
}

// UpdateDependencies amends the list of requested additional operators with any missing dependencies
func (mgr *Manager) UpdateDependencies(cluster *common.Cluster) error {
	operators, err := unmarshallOperators(cluster.Operators)
	if err != nil {
		return err
	}
	operators, err = mgr.resolveDependencies(operators)
	if err != nil {
		return err
	}
	updatedOperators, err := json.Marshal(operators)
	if err != nil {
		return err
	}
	cluster.Operators = string(updatedOperators)

	return nil
}

func (mgr *Manager) resolveDependencies(operators models.Operators) (models.Operators, error) {
	enabledOperators := mgr.getEnabledOperators(operators)
	for _, inputOperator := range operators {
		if enabledOperators[inputOperator.OperatorType] {
			inputOperator.Enabled = swag.Bool(true)
		}
		delete(enabledOperators, inputOperator.OperatorType)
	}
	for enabledOperator := range enabledOperators {
		clusterOperator := models.ClusterOperator{
			OperatorType: enabledOperator,
			Enabled:      swag.Bool(true),
		}
		operators = append(operators, &clusterOperator)
	}
	return operators, nil
}

func (mgr *Manager) getEnabledOperators(operators models.Operators) map[models.OperatorType]bool {
	fifo := list.New()
	visited := make(map[models.OperatorType]bool)
	for _, op := range operators {
		if swag.BoolValue(op.Enabled) {
			visited[op.OperatorType] = true
			for _, dep := range mgr.operators[op.OperatorType].GetDependencies() {
				fifo.PushBack(dep)
			}
		}
	}
	for fifo.Len() > 0 {
		first := fifo.Front()
		op := first.Value.(models.OperatorType)
		for _, dep := range mgr.operators[op].GetDependencies() {
			if !visited[dep] {
				fifo.PushBack(dep)
			}
		}
		visited[op] = true
		fifo.Remove(first)
	}
	return visited
}

func unmarshallOperators(operatorsJson string) (models.Operators, error) {
	operators := make(models.Operators, 0)
	if operatorsJson != "" {
		if err := json.Unmarshal([]byte(operatorsJson), &operators); err != nil {
			return nil, err
		}
	}
	return operators, nil
}

func findOperatorInJson(cluster *common.Cluster, operatorType models.OperatorType) (*models.ClusterOperator, error) {
	operators, err := unmarshallOperators(cluster.Operators)
	if err != nil {
		return nil, err
	}
	return findOperator(operators, operatorType)
}

func findOperator(operators models.Operators, operatorType models.OperatorType) (*models.ClusterOperator, error) {
	for _, operator := range operators {
		if operator.OperatorType == operatorType {
			return operator, nil
		}
	}
	return nil, nil
}

func isEnabled(cluster *common.Cluster, operatorType models.OperatorType) bool {
	operator, _ := findOperatorInJson(cluster, operatorType)
	if operator == nil {
		return false
	}

	return swag.BoolValue(operator.Enabled)
}
