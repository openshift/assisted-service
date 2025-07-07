package metallb

import (
	"context"
	"fmt"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

const (
	clusterValidationID = string(models.ClusterValidationIDMetallbRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDMetallbRequirementsSatisfied)
)

// operator is a MetalLB OLM operator plugin; it implements api.Operator
type operator struct {
	log    logrus.FieldLogger
	Config *Config
}

var Operator = models.MonitoredOperator{
	Name:             "metallb",
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        MetalLBNamespace,
	SubscriptionName: MetalLBSubscriptionName,
	TimeoutSeconds:   30 * 60,
}

// NewMetalLBOperator creates new MetalLB operator
func NewMetalLBOperator(log logrus.FieldLogger) *operator {
	cfg := Config{}
	err := envconfig.Process(common.EnvConfigPrefix, &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return newMetalLBOperatorWithConfig(log, &cfg)
}

// newMetalLBOperatorWithConfig creates new MetalLB operator with given configuration
func newMetalLBOperatorWithConfig(log logrus.FieldLogger, config *Config) *operator {
	return &operator{
		log:    log,
		Config: config,
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

	if ok, _ := common.BaseVersionLessThan(MetalLBMinOpenshiftVersion, cluster.OpenshiftVersion); ok {
		result[0].Status = api.Failure
		result[0].Reasons = []string{
			fmt.Sprintf("MetalLB is only supported for OpenShift versions %s and above", MetalLBMinOpenshiftVersion),
		}
		return result, nil
	}

	return result, nil
}

// ValidateHost validates host requirements for MetalLB
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host, additionalOperatorRequirements *models.ClusterHostRequirementsDetails) (api.ValidationResult, error) {
	if host.Inventory == "" {
		message := "Missing Inventory in the host"
		return api.ValidationResult{Status: api.Pending, ValidationId: o.GetHostValidationID(), Reasons: []string{message}}, nil
	}

	// MetalLB doesn't have specific hardware requirements beyond basic cluster requirements
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

// GetMonitoredOperator returns MonitoredOperator corresponding to MetalLB
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GetHostRequirements provides operator's requirements towards the host
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	// MetalLB has minimal resource requirements
	return &models.ClusterHostRequirementsDetails{
		CPUCores: o.Config.MetalLBCPUPerHost,
		RAMMib:   o.Config.MetalLBMemoryPerHostMiB,
	}, nil
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
				Qualitative: []string{},
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.Config.MetalLBCPUPerHost,
					RAMMib:   o.Config.MetalLBMemoryPerHostMiB,
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Qualitative: []string{},
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.Config.MetalLBCPUPerHost,
					RAMMib:   o.Config.MetalLBMemoryPerHostMiB,
				},
			},
		},
	}, nil
}

// GetSupportedArchitectures returns supported architectures for MetalLB
func (o *operator) GetSupportedArchitectures() []string {
	return []string{
		common.X86CPUArchitecture,
		common.ARM64CPUArchitecture,
	}
}

// GetFeatureSupportID returns the feature support level ID for MetalLB
func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDMETALLB
}

// GetBundleLabels returns bundle labels for MetalLB
func (o *operator) GetBundleLabels() []string {
	return []string{}
}
