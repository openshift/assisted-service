package osc

import (
	"context"
	"fmt"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

type operator struct {
	log    logrus.FieldLogger
	config *Config
}

var Operator = models.MonitoredOperator{
	Name:             Name,
	Namespace:        Namespace,
	OperatorType:     models.OperatorTypeOlm,
	SubscriptionName: SubscriptionName,
	TimeoutSeconds:   60 * 60,
}

func NewOscOperator(log logrus.FieldLogger) *operator {
	cfg := Config{}
	err := envconfig.Process(common.EnvConfigPrefix, &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return &operator{
		log:    log,
		config: &cfg,
	}
}

// GetName reports the name of an operator this Operator manages
func (o *operator) GetName() string {
	return Operator.Name
}

func (o *operator) GetFullName() string {
	return FullName
}

func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	return make([]string, 0), nil
}

// GetClusterValidationID returns cluster validation ID for the Operator
func (o *operator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDOscRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDOscRequirementsSatisfied)
}

// ValidateCluster verifies whether this operator is valid for given cluster
func (o *operator) ValidateCluster(_ context.Context, cluster *common.Cluster) (api.ValidationResult, error) {
	if !featuresupport.IsFeatureCompatibleWithArchitecture(models.FeatureSupportLevelIDOSC, cluster.OpenshiftVersion, cluster.CPUArchitecture) {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{fmt.Sprintf(
			"%s is not supported for %s CPU architecture.", o.GetFullName(), cluster.CPUArchitecture)}}, nil
	}

	if ok, _ := common.BaseVersionLessThan(OscMinOpenshiftVersion, cluster.OpenshiftVersion); ok {
		message := fmt.Sprintf("%s is only supported for openshift versions %s and above", o.GetFullName(), OscMinOpenshiftVersion)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{message}}, nil
	}

	return api.ValidationResult{Status: api.Success, ValidationId: o.GetClusterValidationID(), Reasons: []string{}}, nil
}

// ValidateHost returns validationResult based on node type requirements such as memory and cpu
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host, _ *models.ClusterHostRequirementsDetails) (api.ValidationResult, error) {
	if host.Inventory == "" {
		o.log.Info("Empty Inventory of host with hostID ", host.ID)
		return api.ValidationResult{Status: api.Pending, ValidationId: o.GetHostValidationID(), Reasons: []string{"Missing Inventory in some of the hosts"}}, nil
	}
	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		o.log.Errorf("Failed to get inventory from host with id %s", host.ID)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID()}, err
	}

	requirements, err := o.GetHostRequirements(ctx, cluster, host)
	if err != nil {
		message := fmt.Sprintf("Failed to get host requirements for host with id %s", host.ID)
		o.log.Error(message)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message, err.Error()}}, err
	}

	cpu := requirements.CPUCores
	if inventory.CPU.Count < cpu {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{fmt.Sprintf("Insufficient CPU to deploy %s. Required CPU count is %d but found %d ", o.GetFullName(), cpu, inventory.CPU.Count)}}, nil
	}

	mem := requirements.RAMMib
	memBytes := conversions.MibToBytes(mem)
	if inventory.Memory.UsableBytes < memBytes {
		usableMemory := conversions.BytesToMib(inventory.Memory.UsableBytes)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{fmt.Sprintf("Insufficient memory to deploy %s. Required memory is %d MiB but found %d MiB", o.GetFullName(), mem, usableMemory)}}, nil
	}

	return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID()}, nil
}

// GetHostRequirements provides operator's requirements towards the host
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	log := logutil.FromContext(ctx, o.log)
	preflightRequirements, err := o.GetPreflightRequirements(ctx, cluster)
	if err != nil {
		log.WithError(err).Errorf("Cannot retrieve preflight requirements for host %s", host.ID)
		return nil, err
	}
	role := common.GetEffectiveRole(host)
	switch role {
	case models.HostRoleMaster:
		return preflightRequirements.Requirements.Master.Quantitative, nil
	case models.HostRoleWorker, models.HostRoleAutoAssign:
		return preflightRequirements.Requirements.Worker.Quantitative, nil
	}
	return nil, fmt.Errorf("unsupported role: %s", role)
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
				Qualitative: []string{
					fmt.Sprintf("%d MiB of additional RAM", o.config.OscMemoryPerHostMiB),
					fmt.Sprintf("%d additional CPUs", o.config.OscCPUPerHost),
				},
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.config.OscCPUPerHost,
					RAMMib:   o.config.OscMemoryPerHostMiB,
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Qualitative: []string{
					fmt.Sprintf("%d MiB of additional RAM", o.config.OscMemoryPerHostMiB),
					fmt.Sprintf("%d additional CPUs", o.config.OscCPUPerHost),
				},
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.config.OscCPUPerHost,
					RAMMib:   o.config.OscMemoryPerHostMiB,
				},
			},
		},
	}, nil
}

// GetProperties provides description of operator properties: none required
func (l *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to the MTV
func (l *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GenerateManifests generates manifests for the operator
func (o *operator) GenerateManifests(cluster *common.Cluster) (map[string][]byte, []byte, error) {
	return Manifests()
}

func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDOSC
}

// GetBundleLabels returns the bundle labels for the OSC operator
func (o *operator) GetBundleLabels() []string {
	return []string(Operator.Bundles)
}
