package operators

import (
	"encoding/json"

	"github.com/go-openapi/swag"
	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

// Manager is responsible for performing operations against additional operators
type Manager struct {
	log                logrus.FieldLogger
	ocsValidatorConfig *ocs.Config
	ocsValidator       ocs.OcsValidator
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
}

// NewManager creates new instance of an Operator Manager
func NewManager(log logrus.FieldLogger) Manager {
	cfg := ocs.Config{}
	err := envconfig.Process("myapp", &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return NewManagerWithConfig(log, &cfg)
}

// NewManagerWithConfig creates new instance of an Operator Manager
func NewManagerWithConfig(log logrus.FieldLogger, cfg *ocs.Config) Manager {
	ocsValidator := ocs.NewOCSValidator(log.WithField("pkg", "ocs-operator-state"), cfg)
	return Manager{
		log:                log,
		ocsValidatorConfig: cfg,
		ocsValidator:       ocsValidator,
	}
}

// GenerateManifests generates manifests for all enabled operators.
// Returns map assigning manifest content to its desired file name
func (mgr *Manager) GenerateManifests(cluster *common.Cluster) (map[string]string, error) {
	lsoEnabled := false
	ocsEnabled := mgr.checkOCSEnabled(cluster)
	if ocsEnabled {
		lsoEnabled = true // if OCS is enabled, LSO must be enabled by default
	} else {
		lsoEnabled = mgr.checkLSOEnabled(cluster)
	}
	operatorManifests := make(map[string]string)

	if lsoEnabled {
		manifests, err := mgr.generateLSOManifests(cluster)
		if err != nil {
			mgr.log.Error("Cannot generate LSO manifests due to ", err)
			return nil, err
		}
		for k, v := range manifests {
			operatorManifests[k] = v
		}
	}

	if ocsEnabled {
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
	return mgr.checkLSOEnabled(cluster) || mgr.checkOCSEnabled(cluster)
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
	operator, err := findOperator(cluster, operatorType)
	if err != nil {
		return "Something went wrong with Unmarshalling operators"
	}
	if operator != nil {
		if swag.BoolValue(operator.Enabled) {
			return operator.Status
		}
	}
	return "OCS is disabled"
}

func (mgr *Manager) generateLSOManifests(cluster *common.Cluster) (map[string]string, error) {
	mgr.log.Info("Creating LSO Manifests")
	return lso.Manifests(cluster.OpenshiftVersion)
}

func (mgr *Manager) generateOCSManifests(cluster *common.Cluster) (map[string]string, error) {
	mgr.log.Info("Creating OCS Manifests")
	return ocs.Manifests(mgr.ocsValidatorConfig.OCSMinimalDeployment, mgr.ocsValidatorConfig.OCSDisksAvailable, len(cluster.Cluster.Hosts))
}

func (mgr *Manager) checkLSOEnabled(cluster *common.Cluster) bool {
	return isEnabled(cluster, models.OperatorTypeLso)
}

func (mgr *Manager) checkOCSEnabled(cluster *common.Cluster) bool {
	return isEnabled(cluster, models.OperatorTypeOcs)
}

func findOperator(cluster *common.Cluster, operatorType models.OperatorType) (*models.ClusterOperator, error) {
	if cluster.Operators != "" {
		var operators models.Operators
		if err := json.Unmarshal([]byte(cluster.Operators), &operators); err != nil {
			return nil, err
		}
		for _, operator := range operators {
			if operator.OperatorType == operatorType {
				return operator, nil
			}
		}
	}
	return nil, nil
}

func isEnabled(cluster *common.Cluster, operatorType models.OperatorType) bool {
	operator, _ := findOperator(cluster, operatorType)
	if operator == nil {
		return false
	}

	return swag.BoolValue(operator.Enabled)
}
