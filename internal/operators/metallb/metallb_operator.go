package metallb

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/go-version"
	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

// api.Operator implementation for MetalLB
type operator struct {
	log       logrus.FieldLogger
	config    *Config
	extracter oc.Extracter
}

var Operator = models.MonitoredOperator{
	Name:             "metallb",
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        "metallb-system",
	SubscriptionName: "metallb-operator",
	TimeoutSeconds:   30 * 60,
}

// NewMetalLBOperator creates new LvmOperator
func NewMetalLBOperator(log logrus.FieldLogger, extracter oc.Extracter) *operator {
	cfg := Config{}
	err := envconfig.Process(common.EnvConfigPrefix, &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return newMetalLBOperatorWithConfig(log, &cfg, extracter)
}

// newMetalLBOperatorWithConfig creates new ODFOperator with given configuration
func newMetalLBOperatorWithConfig(log logrus.FieldLogger, config *Config, extracter oc.Extracter) *operator {
	return &operator{
		log:       log,
		config:    config,
		extracter: extracter,
	}
}

// GetName reports the name of an operator this Operator manages
func (o *operator) GetName() string {
	return Operator.Name
}

// GetFullName reports the full name of an operator this Operator manages
func (o *operator) GetFullName() string {
	return Operator.Name
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	return make([]string, 0), nil
}

// GetClusterValidationID returns cluster validation ID for the Operator
func (o *operator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDMetallbRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDMetallbRequirementsSatisfied)
}

// ValidateCluster always return "valid" result
func (o *operator) ValidateCluster(_ context.Context, cluster *common.Cluster) (api.ValidationResult, error) {
	var ocpVersion, minOpenshiftVersionForMetalLB *version.Version
	var err error

	ocpVersion, err = version.NewVersion(cluster.OpenshiftVersion)
	if err != nil {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{err.Error()}}, nil
	}
	minOpenshiftVersionForMetalLB, err = version.NewVersion(o.config.MetalLBMinOpenshiftVersion)
	if err != nil {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{err.Error()}}, nil
	}
	if ocpVersion.LessThan(minOpenshiftVersionForMetalLB) {
		message := fmt.Sprintf("MetalLB operator is only supported for openshift versions %s and above", o.config.MetalLBMinOpenshiftVersion)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{message}}, nil
	}

	_, err = parsePropertiesField(cluster)
	if err != nil {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{err.Error()}}, nil
	}

	return api.ValidationResult{Status: api.Success, ValidationId: o.GetClusterValidationID()}, nil
}

// ValidateHost always return "valid" result
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host) (api.ValidationResult, error) {
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID()}, nil
}

// GenerateManifests generates manifests for the operator
func (o *operator) GenerateManifests(cluster *common.Cluster) (map[string][]byte, []byte, error) {
	return Manifests(cluster)
}

// GetProperties provides description of operator properties: none required
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to the LSO
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GetHostRequirements provides operator's requirements towards the host
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster, _ *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	log := logutil.FromContext(ctx, o.log)
	preflightRequirements, err := o.GetPreflightRequirements(ctx, cluster)
	if err != nil {
		log.WithError(err).Errorf("Cannot retrieve preflight requirements for cluster %s", cluster.ID)
		return nil, err
	}
	return preflightRequirements.Requirements.Master.Quantitative, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(context context.Context, cluster *common.Cluster) (*models.OperatorHardwareRequirements, error) {
	dependecies, err := o.GetDependencies(cluster)
	if err != nil {
		return &models.OperatorHardwareRequirements{}, err
	}
	return &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
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

func (o *operator) GetSupportedArchitectures() []string {
	return []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture}
}

// parse and validate the operator's properties field
func parsePropertiesField(cluster *common.Cluster) (Properties, error) {
	propertiesObj := Properties{}
	properties := ""

	for _, mo := range cluster.MonitoredOperators {
		if mo != nil && mo.Name == Operator.Name {
			properties = mo.Properties
			break
		}
	}

	if properties == "" {
		return propertiesObj, fmt.Errorf("Properties field for operator %s can not be empty", Operator.Name)
	}

	err := json.Unmarshal([]byte(properties), &propertiesObj)
	if err != nil {
		return propertiesObj, err
	}

	if !network.IsIPv4Addr(propertiesObj.ApiIP) {
		return propertiesObj, fmt.Errorf("MetalLB API IP is not a valid IPv4 address")
	}

	if !network.IsIPv4Addr(propertiesObj.IngressIP) {
		return propertiesObj, fmt.Errorf("MetalLB Ingress IP is not a valid IPv4 address")
	}

	return propertiesObj, nil
}
