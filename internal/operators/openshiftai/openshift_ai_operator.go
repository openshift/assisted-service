package openshiftai

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/kelseyhightower/envconfig"
	"github.com/lib/pq"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/authorino"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/operators/nvidiagpu"
	"github.com/openshift/assisted-service/internal/operators/odf"
	"github.com/openshift/assisted-service/internal/operators/pipelines"
	"github.com/openshift/assisted-service/internal/operators/serverless"
	"github.com/openshift/assisted-service/internal/operators/servicemesh"
	"github.com/openshift/assisted-service/internal/templating"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
)

var Operator = models.MonitoredOperator{
	Namespace:        "redhat-ods-operator",
	Name:             "openshift-ai",
	OperatorType:     models.OperatorTypeOlm,
	SubscriptionName: "rhods-operator",
	TimeoutSeconds:   30 * 60,
	Bundles:          pq.StringArray{operatorscommon.BundleOpenshiftai},
}

// operator is an OpenShift AI OLM operator plugin.
type operator struct {
	log       logrus.FieldLogger
	config    *Config
	templates *template.Template
}

// NewOpenShiftAIOperator creates new OpenShift AI operator.
func NewOpenShiftAIOperator(log logrus.FieldLogger) *operator {
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
	// TODO: We shold probably add the node feature discovery and NVIDIA GPU operators only if there is at least one
	// NVIDIA GPU in the cluster, but unfortunatelly this is calculated and saved to the database only when the
	// cluster is created or updated via the API, and at that point we don't have the host inventory yet to
	// determine if there are NVIDIA GPU.
	result = []string{
		authorino.Operator.Name,
		nvidiagpu.Operator.Name,
		odf.Operator.Name,
		pipelines.Operator.Name,
		serverless.Operator.Name,
		servicemesh.Operator.Name,
	}
	return result, nil
}

// GetClusterValidationID returns cluster validation ID for the operator.
func (o *operator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDOpenshiftAiRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the operator.
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDOpenshiftAiRequirementsSatisfied)
}

// ValidateCluster checks if the cluster satisfies the requirements to install the operator.
func (o *operator) ValidateCluster(ctx context.Context, cluster *common.Cluster) (result api.ValidationResult,
	err error) {
	result.ValidationId = o.GetClusterValidationID()

	// Check the number of worker nodes:
	workerCount := int64(common.NumberOfWorkers(cluster))
	if workerCount < o.config.MinWorkerNodes {
		result.Reasons = append(
			result.Reasons,
			fmt.Sprintf(
				"OpenShift AI requires at least %d worker nodes, but the cluster has %d.",
				o.config.MinWorkerNodes, workerCount,
			),
		)
	}

	// Check that there is at least one supported GPU:
	if o.config.RequireGPU {
		var gpuList []*models.Gpu
		gpuList, err = o.gpusInCluster(cluster)
		if err != nil {
			return
		}
		var supportedGpuCount int64
		for _, gpu := range gpuList {
			var isSupported bool
			isSupported, err = o.isSupportedGpu(gpu)
			if err != nil {
				return
			}
			if isSupported {
				supportedGpuCount++
			}
		}
		if supportedGpuCount == 0 {
			result.Reasons = append(
				result.Reasons,
				"OpenShift AI requires at least one supported GPU, but there is none in the "+
					"discovered hosts.",
			)
		}
	}

	if len(result.Reasons) > 0 {
		result.Status = api.Failure
	} else {
		result.Status = api.Success
	}
	return
}

func (o *operator) gpusInCluster(cluster *common.Cluster) (result []*models.Gpu, err error) {
	for _, host := range cluster.Hosts {
		var gpus []*models.Gpu
		gpus, err = o.gpusInHost(host)
		if err != nil {
			return
		}
		result = append(result, gpus...)
	}
	return
}

func (o *operator) gpusInHost(host *models.Host) (result []*models.Gpu, err error) {
	if host.Inventory == "" {
		return
	}
	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		return
	}
	result = inventory.Gpus
	return
}

func (o *operator) isSupportedGpu(gpu *models.Gpu) (result bool, err error) {
	for _, supportedGpu := range o.config.SupportedGPUs {
		if strings.EqualFold(gpu.VendorID, supportedGpu) {
			result = true
			return
		}
	}
	return
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
