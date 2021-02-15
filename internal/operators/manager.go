package operators

import (
	"encoding/json"
	"fmt"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

// Manager is responsible for performing operations against additional operators
type Manager struct {
	log                logrus.FieldLogger
	ocsValidatorConfig *ocs.Config
	ocsValidator       ocs.OCSValidator
	operators          map[models.OperatorType]api.Operator
}

//go:generate  mockgen -package=operators -destination=mock_operators_api.go . API
type API interface {
	// ValidateOCSRequirements validates OCS requirements
	ValidateOCSRequirements(cluster *common.Cluster) string
	// GenerateManifests generates manifests for all enabled operators.
	// Returns map assigning manifest content to its desired file name
	GenerateManifests(cluster *common.Cluster) (map[string]string, error)
	// AnyOperatorEnabled checks whether any operator has been enabled for the given cluster
	AnyOperatorEnabled(cluster *common.Cluster) bool
	// GetOperatorStatus gets status of an operator of given type.
	GetOperatorStatus(cluster *common.Cluster, operatorType models.OperatorType) string
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

	// Generate manifests for OCS
	// TODO: migrate OCS to generic API and remove this code
	if mgr.checkOCSEnabled(cluster) {
		manifests, err := mgr.generateOCSManifests(cluster)
		if err != nil {
			mgr.log.Error("Cannot generate OCS manifests due to ", err)
			return nil, err
		}
		for k, v := range manifests {
			operatorManifests[k] = v
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
	return mgr.checkOCSEnabled(cluster)
}

// ValidateOCSRequirements validates OCS requirements. Returns "true" if OCS operator is not deployed
func (mgr *Manager) ValidateOCSRequirements(cluster *common.Cluster) string {
	if isEnabled(cluster, models.OperatorTypeOcs) {
		return mgr.ocsValidator.ValidateOCSRequirements(&cluster.Cluster)
	}
	return "success"
}

// GetOperatorStatus gets status of an operator of given type.
func (mgr *Manager) GetOperatorStatus(cluster *common.Cluster, operatorType models.OperatorType) string {
	operator, err := findOperatorInJson(cluster, operatorType)
	if err != nil {
		return "Something went wrong with Unmarshalling operators"
	}
	if operator != nil {
		if swag.BoolValue(operator.Enabled) {
			return operator.Status
		}
	}
	return fmt.Sprintf("%v is disabled", operatorType)
}

// UpdateDependencies amends the list of requested additional operators with any missing dependencies
func (mgr *Manager) UpdateDependencies(cluster *common.Cluster) error {
	operators, err := unmarshallOperators(cluster.Operators)
	if err != nil {
		return err
	}
	operators = mgr.resolveDependencies(operators)
	updatedOperators, err := json.Marshal(operators)
	if err != nil {
		return err
	}
	cluster.Operators = string(updatedOperators)

	return nil
}

func (mgr *Manager) resolveDependencies(operators models.Operators) models.Operators {
	// TODO: temporary code; make it generic when both LSO and OCS are migrated
	mapping := make(map[models.OperatorType]*models.ClusterOperator)
	for _, operator := range operators {
		mapping[operator.OperatorType] = operator
	}
	var ocsEnabled bool
	finalOperators := make(models.Operators, 0, len(operators))
	ocsOperator := mapping[models.OperatorTypeOcs]
	if ocsOperator != nil {
		if swag.BoolValue(ocsOperator.Enabled) {
			ocsEnabled = true
		}
		finalOperators = append(finalOperators, ocsOperator)
	}
	lsOperator := mapping[models.OperatorTypeLso]
	if ocsEnabled {
		if lsOperator == nil {
			lsOperator = &models.ClusterOperator{Enabled: swag.Bool(true), OperatorType: models.OperatorTypeLso}
		} else {
			lsOperator.Enabled = swag.Bool(true)
		}
	}
	if lsOperator != nil {
		finalOperators = append(finalOperators, lsOperator)
	}

	return finalOperators
}

func (mgr *Manager) generateOCSManifests(cluster *common.Cluster) (map[string]string, error) {
	mgr.log.Info("Creating OCS Manifests")
	return ocs.Manifests(mgr.ocsValidatorConfig.OCSMinimalDeployment, mgr.ocsValidatorConfig.OCSDisksAvailable, len(cluster.Cluster.Hosts))
}

func (mgr *Manager) checkOCSEnabled(cluster *common.Cluster) bool {
	return isEnabled(cluster, models.OperatorTypeOcs)
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
