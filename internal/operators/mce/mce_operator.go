package mce

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-version"
	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

// operator is an MCE OLM operator plugin.
type operator struct {
	log    logrus.FieldLogger
	config *Config
}

var Operator = models.MonitoredOperator{
	Name:             "mce",
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        "multicluster-engine",
	SubscriptionName: "multicluster-engine",
	TimeoutSeconds:   60 * 60,
}

// NewMceOperator creates new MCE operator.
func NewMceOperator(log logrus.FieldLogger) *operator {
	cfg := Config{}
	err := envconfig.Process(common.EnvConfigPrefix, &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}

	return newMceOperatorWithConfig(log, &cfg)
}

// newMceOperatorWithConfig creates new MCE operator with the given configuration.
func newMceOperatorWithConfig(log logrus.FieldLogger, config *Config) *operator {
	return &operator{
		log:    log,
		config: config,
	}
}

// GetName reports the name of an operator.
func (o *operator) GetName() string {
	return Operator.Name
}

func (o *operator) GetFullName() string {
	return "multicluster engine"
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	return make([]string, 0), nil
}

// GetClusterValidationID returns cluster validation ID for the operator.
func (o *operator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDMceRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the operator.
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDMceRequirementsSatisfied)
}

// ValidateCluster checks if the cluster satisfies the requirements to install the operator.
func (o *operator) ValidateCluster(_ context.Context, cluster *common.Cluster) (api.ValidationResult, error) {
	var ocpVersion, minOpenshiftVersionForMce *version.Version
	var err error

	ocpVersion, err = version.NewVersion(cluster.OpenshiftVersion)
	if err != nil {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{err.Error()}}, nil
	}

	minOpenshiftVersionForMce, err = version.NewVersion(o.config.MceMinOpenshiftVersion)

	if err != nil {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{err.Error()}}, nil
	}

	if ocpVersion.LessThan(minOpenshiftVersionForMce) {
		message := fmt.Sprintf("multicluster engine is only supported for openshift versions %s and above", o.config.MceMinOpenshiftVersion)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{message}}, nil
	}

	return api.ValidationResult{Status: api.Success, ValidationId: o.GetClusterValidationID()}, nil
}

// ValidateHost returns validationResult based on node type requirements such as memory and cpu
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host) (result api.ValidationResult, err error) {
	if host.Inventory == "" {
		message := "Missing Inventory in the host"
		return api.ValidationResult{Status: api.Pending, ValidationId: o.GetHostValidationID(), Reasons: []string{message}}, nil
	}

	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		message := "Failed to get inventory from host"
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message}}, err
	}

	requirements, err := o.GetHostRequirements(ctx, cluster, host)
	if err != nil {
		message := fmt.Sprintf("Failed to get host requirements for host with id %s", host.ID)
		o.log.Error(message)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message, err.Error()}}, err
	}

	cpu := requirements.CPUCores
	if inventory.CPU.Count < cpu {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{fmt.Sprintf("Insufficient CPU to deploy multicluster engine. Required CPU count is %d but found %d ", cpu, inventory.CPU.Count)}}, nil
	}

	mem := requirements.RAMMib
	memBytes := conversions.MibToBytes(mem)
	if inventory.Memory.UsableBytes < memBytes {
		usableMemory := conversions.BytesToMib(inventory.Memory.UsableBytes)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{fmt.Sprintf("Insufficient memory to deploy multicluster engine. Required memory is %d MiB but found %d MiB", mem, usableMemory)}}, nil
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
	return &Operator
}

// GetHostRequirements provides the requirements that the host needs to satisfy in order to be able to install the operator.
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	log := logutil.FromContext(ctx, o.log)
	preflightRequirements, err := o.GetPreflightRequirements(ctx, cluster)
	if err != nil {
		log.WithError(err).Errorf("Cannot retrieve preflight requirements for cluster %s", cluster.ID)
		return nil, err
	}

	if common.IsSingleNodeCluster(cluster) {
		// SNO req
		return &models.ClusterHostRequirementsDetails{
			CPUCores: SNOMinimumCpu,
			RAMMib:   conversions.GibToMib(SNOMinimumMemory),
		}, nil
	}

	return preflightRequirements.Requirements.Worker.Quantitative, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(context context.Context, cluster *common.Cluster) (*models.OperatorHardwareRequirements, error) {
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
					CPUCores: MinimumCPU,
					RAMMib:   conversions.GibToMib(MinimumMemory),
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Qualitative: []string{},
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: MinimumCPU,
					RAMMib:   conversions.GibToMib(MinimumMemory),
				},
			},
		},
	}, nil
}

func (o *operator) GetSupportedArchitectures() []string {
	return []string{common.X86CPUArchitecture, common.PowerCPUArchitecture,
		common.S390xCPUArchitecture, common.ARM64CPUArchitecture, common.AMD64CPUArchitecture,
	}

}

func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDMCE
}

func GetMinDiskSizeGB(cluster *models.Cluster) int64 {
	if common.IsSingleNodeCluster(&common.Cluster{Cluster: *cluster}) {
		return lo.Sum(lo.Values(storageSizeGi))
	}
	return lo.Max(lo.Values(storageSizeGi))
}
