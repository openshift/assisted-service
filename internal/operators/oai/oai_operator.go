package oai

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-version"
	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
)

const (
	OperatorName      = "oai"
	operatorNamespace = "redhat-ods-operator"
	operatorFullName  = "OpenShift AI"
	subscriptionName  = "rhods-operator"
)

// operator is an OpenShift AI OLM operator plugin.
type operator struct {
	log    logrus.FieldLogger
	config *Config
}

// NewOperator creates new OpenShift AI operator.
func NewOperator(log logrus.FieldLogger) *operator {
	config := &Config{}
	err := envconfig.Process(common.EnvConfigPrefix, config)
	if err != nil {
		log.Fatal(err.Error())
	}
	return &operator{
		log:    log,
		config: config,
	}
}

// GetName reports the name of an operator.
func (o *operator) GetName() string {
	return OperatorName
}

func (o *operator) GetFullName() string {
	return operatorFullName
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(c *common.Cluster) ([]string, error) {
	return make([]string, 0), nil
}

// GetClusterValidationID returns cluster validation ID for the operator.
func (o *operator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDOAIRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the operator.
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDOAIRequirementsSatisfied)
}

// ValidateCluster checks if the cluster satisfies the requirements to install the operator.
func (o *operator) ValidateCluster(ctx context.Context, cluster *common.Cluster) (result api.ValidationResult,
	err error) {
	// Check the OpenShift version:
	curVersion, err := version.NewVersion(cluster.OpenshiftVersion)
	if err != nil {
		result.Status = api.Failure
		result.ValidationId = o.GetHostValidationID()
		result.Reasons = []string{
			err.Error(),
		}
		return
	}
	minVersion, err := version.NewVersion(o.config.MinOpenShiftVersion)
	if err != nil {
		result.Status = api.Failure
		result.ValidationId = o.GetHostValidationID()
		result.Reasons = []string{
			err.Error(),
		}
		return
	}
	if curVersion.LessThan(minVersion) {
		result.Status = api.Failure
		result.ValidationId = o.GetClusterValidationID()
		result.Reasons = []string{
			fmt.Sprintf(
				"OpenShift AI is only supported for OpenShift version '%s' and above",
				o.config.MinOpenShiftVersion,
			),
		}
		return
	}

	// Check the number of worker nodes:
	workerNodes := o.numberOfWorkers(cluster)
	if workerNodes < o.config.MinWorkerNodes {
		result.Status = api.Failure
		result.ValidationId = o.GetClusterValidationID()
		result.Reasons = []string{
			fmt.Sprintf(
				"OpenShift AI requires at least %d worker nodes, but the cluster only has %d",
				o.config.MinWorkerNodes, workerNodes,
			),
		}
		return
	}

	result.Status = api.Success
	result.ValidationId = o.GetClusterValidationID()
	return
}

func (o *operator) numberOfWorkers(cluster *common.Cluster) int64 {
	var result int64
	for _, host := range cluster.Hosts {
		if host.Role == models.HostRoleWorker {
			result++
		}
	}
	return result
}

// ValidateHost returns validationResult based on node type requirements such as memory and CPU.
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host,
	_ *models.ClusterHostRequirementsDetails) (result api.ValidationResult, err error) {
	if host.Inventory == "" {
		result.Status = api.Pending
		result.ValidationId = o.GetHostValidationID()
		result.Reasons = []string{
			"Missing inventory in the host",
		}
		return
	}
	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		result.Status = api.Pending
		result.ValidationId = o.GetHostValidationID()
		result.Reasons = []string{
			"Failed to get inventory from host",
		}
		return
	}

	requirements, err := o.GetHostRequirements(ctx, cluster, host)
	if err != nil {
		result.Status = api.Pending
		result.ValidationId = o.GetHostValidationID()
		result.Reasons = []string{
			fmt.Sprintf(
				"Failed to get host requirements for host with id '%s'",
				host.ID,
			),
		}
		return
	}

	requiredCPUCores := requirements.CPUCores
	usableCPUCores := inventory.CPU.Count
	if usableCPUCores < requiredCPUCores {
		result.Status = api.Failure
		result.ValidationId = o.GetHostValidationID()
		result.Reasons = []string{
			fmt.Sprintf(
				"Insufficient CPU to deploy OpenShift AI, requires %d CPU cores but found %d",
				requiredCPUCores, usableCPUCores,
			),
		}
		return
	}

	requiredMemoryBytes := conversions.MibToBytes(requirements.RAMMib)
	usableMemoryBytes := inventory.Memory.UsableBytes
	if usableMemoryBytes < requiredMemoryBytes {
		result.Status = api.Failure
		result.ValidationId = o.GetHostValidationID()
		result.Reasons = []string{
			fmt.Sprintf(
				"Insufficient memory to deploy OpenShift AI, requires %d GiB but found %d GiB",
				conversions.BytesToGib(requiredMemoryBytes),
				conversions.BytesToGib(usableMemoryBytes),
			),
		}
		return
	}

	result = api.ValidationResult{
		Status:       api.Success,
		ValidationId: o.GetHostValidationID(),
	}
	return
}

// GenerateManifests generates manifests for the operator.
func (o *operator) GenerateManifests(_ *common.Cluster) (map[string][]byte, []byte, error) {
	return Manifests()
}

// GetProperties provides description of operator properties.
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns the information that describes how to monitor the operator.
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &models.MonitoredOperator{
		Name:             OperatorName,
		Namespace:        operatorNamespace,
		OperatorType:     models.OperatorTypeOlm,
		SubscriptionName: subscriptionName,
		TimeoutSeconds:   60 * 60,
	}
}

// GetHostRequirements provides the requirements that the host needs to satisfy in order to be able to install the
// operator.
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster,
	host *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	preflightRequirements, err := o.GetPreflightRequirements(ctx, cluster)
	if err != nil {
		o.log.WithError(err).Errorf("Cannot retrieve preflight requirements for cluster %s", cluster.ID)
		return nil, err
	}
	return preflightRequirements.Requirements.Worker.Quantitative, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only.
func (o *operator) GetPreflightRequirements(context context.Context,
	cluster *common.Cluster) (result *models.OperatorHardwareRequirements, err error) {
	dependencies, err := o.GetDependencies(cluster)
	if err != nil {
		return
	}
	result = &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: dependencies,
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Worker: &models.HostTypeHardwareRequirements{
				Qualitative: []string{},
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.config.MinCPUCores,
					RAMMib:   conversions.GibToMib(o.config.MinMemoryGiB),
				},
			},
		},
	}
	return
}

func (o *operator) GetSupportedArchitectures() []string {
	return []string{
		common.X86CPUArchitecture,
	}
}

func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDOAI
}
