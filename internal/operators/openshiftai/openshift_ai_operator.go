package openshiftai

import (
	"context"
	"fmt"
	"text/template"

	"github.com/kelseyhightower/envconfig"
	"github.com/lib/pq"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/templating"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
)

const (
	clusterValidationID    = string(models.ClusterValidationIDOpenshiftAiRequirementsSatisfied)
	clusterGPUValidationID = string(models.ClusterValidationIDOpenshiftAiGpuRequirementsSatisfied)
	hostValidationID       = string(models.HostValidationIDOpenshiftAiRequirementsSatisfied)
)

var Operator = models.MonitoredOperator{
	Namespace:        "redhat-ods-operator",
	Name:             "openshift-ai",
	OperatorType:     models.OperatorTypeOlm,
	SubscriptionName: "rhods-operator",
	TimeoutSeconds:   30 * 60,
	Bundles: pq.StringArray{
		operatorscommon.BundleOpenShiftAI.ID,
	},
}

// operator is an OpenShift AI OLM operator plugin.
type operator struct {
	log       logrus.FieldLogger
	config    *Config
	templates *template.Template
	vendors   []GPUVendor
}

//go:generate mockgen --build_flags=--mod=mod -package openshiftai -destination mock_gpu_vendor.go . GPUVendor
type GPUVendor interface {
	ClusterHasGPU(c *common.Cluster) (bool, error)
	GetName() string
	GetFeatureSupportID() models.FeatureSupportLevelID
}

// NewOpenShiftAIOperator creates new OpenShift AI operator.
func NewOpenShiftAIOperator(log logrus.FieldLogger, vendors ...GPUVendor) *operator {
	config := &Config{}
	err := envconfig.Process(common.EnvConfigPrefix, config)
	if err != nil {
		log.Fatal(err.Error())
	}
	templates, err := templating.LoadTemplates(templatesRoot)
	if err != nil {
		log.Fatal(err.Error())
	}
	return &operator{
		log:       log,
		config:    config,
		templates: templates,
		vendors:   vendors,
	}
}

// GetName reports the name of an operator.
func (o *operator) GetName() string {
	return Operator.Name
}

func (o *operator) GetFullName() string {
	return "OpenShift AI"
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(c *common.Cluster) (result []string, err error) {
	ret := make([]string, 0)

	// If there is no hosts in the cluster, add all vendors as dependencies
	if len(c.Hosts) == 0 {
		for _, vendor := range o.vendors {
			ret = append(ret, vendor.GetName())
		}

		return ret, nil
	}

	for _, vendor := range o.vendors {
		hasGPU, err := vendor.ClusterHasGPU(c)
		if err != nil {
			return ret, fmt.Errorf("failed to check if cluster has GPU for %s: %w", vendor.GetName(), err)
		}

		if !hasGPU {
			continue
		}

		ret = append(ret, vendor.GetName())
	}

	return ret, nil
}

func (o *operator) GetDependenciesFeatureSupportID() []models.FeatureSupportLevelID {
	ret := make([]models.FeatureSupportLevelID, 0, len(o.vendors))

	for _, vendor := range o.vendors {
		ret = append(ret, vendor.GetFeatureSupportID())
	}

	return ret
}

// GetClusterValidationIDs returns cluster validation IDs for the operator.
func (o *operator) GetClusterValidationIDs() []string {
	return []string{clusterValidationID}
}

// GetHostValidationID returns host validation ID for the operator.
func (o *operator) GetHostValidationID() string {
	return hostValidationID
}

// ValidateCluster checks if the cluster satisfies the requirements to install the operator.
func (o *operator) ValidateCluster(ctx context.Context, cluster *common.Cluster) ([]api.ValidationResult, error) {
	workerValidation := o.validateWorkers(cluster)

	gpuValidation, err := o.validateGPU(cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to validate GPU: %w", err)
	}

	return []api.ValidationResult{workerValidation, gpuValidation}, nil
}

func (o *operator) validateWorkers(cluster *common.Cluster) api.ValidationResult {
	result := api.ValidationResult{
		ValidationId: clusterValidationID,
		Status:       api.Success,
	}

	// Check the number of worker nodes:
	workerCount := int64(common.NumberOfWorkers(cluster))
	if workerCount < o.config.MinWorkerNodes {
		result.Status = api.Failure
		result.Reasons = []string{
			fmt.Sprintf(
				"OpenShift AI requires at least %d worker nodes, but the cluster has %d.",
				o.config.MinWorkerNodes, workerCount,
			),
		}

		return result
	}

	return result
}

func (o *operator) validateGPU(cluster *common.Cluster) (api.ValidationResult, error) {
	result := api.ValidationResult{
		ValidationId: clusterGPUValidationID,
		Status:       api.Success,
	}

	for _, vendor := range o.vendors {
		hasGPU, err := vendor.ClusterHasGPU(cluster)
		if err != nil {
			return result, fmt.Errorf("failed to check if cluster has GPU for %s: %w", vendor.GetName(), err)
		}

		if hasGPU {
			return result, nil
		}
	}

	result.Status = api.Failure
	result.Reasons = []string{"Cluster doesn't have any supported GPU"}

	return result, nil
}

// ValidateHost returns validationResult based on node type requirements such as memory and CPU.
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host,
	_ *models.ClusterHostRequirementsDetails) (result api.ValidationResult, err error) {
	result.ValidationId = o.GetHostValidationID()

	if host.Inventory == "" {
		result.Status = api.Pending
		result.Reasons = []string{
			"Missing inventory in the host",
		}
		return
	}
	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		result.Status = api.Pending
		result.Reasons = []string{
			"Failed to get inventory from host",
		}
		return
	}

	requirements, err := o.GetHostRequirements(ctx, cluster, host)
	if err != nil {
		result.Status = api.Pending
		result.Reasons = []string{
			fmt.Sprintf(
				"Failed to get host requirements for host with id '%s'",
				host.ID,
			),
		}
		return
	}

	// Check CPU:
	requiredCPUCores := requirements.CPUCores
	usableCPUCores := inventory.CPU.Count
	if usableCPUCores < requiredCPUCores {
		result.Reasons = append(
			result.Reasons,
			fmt.Sprintf(
				"Insufficient CPU to deploy OpenShift AI, requires %d CPU cores but found %d",
				requiredCPUCores, usableCPUCores,
			),
		)
	}

	// Check memory:
	requiredMemoryBytes := conversions.MibToBytes(requirements.RAMMib)
	usableMemoryBytes := inventory.Memory.UsableBytes
	if usableMemoryBytes < requiredMemoryBytes {
		result.Reasons = append(
			result.Reasons,
			fmt.Sprintf(
				"Insufficient memory to deploy OpenShift AI, requires %d GiB but found %d GiB",
				conversions.BytesToGib(requiredMemoryBytes),
				conversions.BytesToGib(usableMemoryBytes),
			),
		)
	}

	if len(result.Reasons) > 0 {
		result.Status = api.Failure
	} else {
		result.Status = api.Success
	}
	return
}

// GetProperties provides description of operator properties.
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns the information that describes how to monitor the operator.
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GetHostRequirements provides the requirements that the host needs to satisfy in order to be able to install the
// operator.
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster,
	host *models.Host) (result *models.ClusterHostRequirementsDetails, err error) {
	preflightRequirements, err := o.GetPreflightRequirements(ctx, cluster)
	if err != nil {
		o.log.WithError(err).Errorf("Cannot retrieve preflight requirements for cluster %s", cluster.ID)
		return
	}
	switch common.GetEffectiveRole(host) {
	case models.HostRoleMaster:
		result = preflightRequirements.Requirements.Master.Quantitative
	case models.HostRoleWorker:
		result = preflightRequirements.Requirements.Worker.Quantitative
	default:
		result = &models.ClusterHostRequirementsDetails{}
	}
	return
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
			Master: &models.HostTypeHardwareRequirements{
				Qualitative: []string{},
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Qualitative: []string{},
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.config.MinWorkerCPUCores,
					RAMMib:   conversions.GibToMib(o.config.MinWorkerMemoryGiB),
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
	return models.FeatureSupportLevelIDOPENSHIFTAI
}

// GetBundleLabels returns the bundle labels for the LSO operator
func (l *operator) GetBundleLabels() []string {
	return []string(Operator.Bundles)
}
