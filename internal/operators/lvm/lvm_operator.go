package lvm

import (
	"context"
	"fmt"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

const (
	clusterValidationID = string(models.ClusterValidationIDLvmRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDLvmRequirementsSatisfied)
)

// operator is an ODF LVM OLM operator plugin; it implements api.Operator
type operator struct {
	log    logrus.FieldLogger
	Config *Config
}

const defaultStorageClassName = "lvms-" + defaultDeviceName

var Operator = models.MonitoredOperator{
	Name:             "lvm",
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        "openshift-storage",
	SubscriptionName: "",
	TimeoutSeconds:   30 * 60,
}

// NewLvmOperator creates new LvmOperator
func NewLvmOperator(log logrus.FieldLogger) *operator {
	cfg := Config{}
	err := envconfig.Process(common.EnvConfigPrefix, &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return newLvmOperatorWithConfig(log, &cfg)
}

// newOdfOperatorWithConfig creates new ODFOperator with given configuration
func newLvmOperatorWithConfig(log logrus.FieldLogger, config *Config) *operator {
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
	return "Logical Volume Management"
}

func (o *operator) StorageClassName() string {
	return defaultStorageClassName
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	return make([]string, 0), nil
}

func (o *operator) GetDependenciesFeatureSupportID() []models.FeatureSupportLevelID {
	return nil
}

// GetClusterValidationIDs returns cluster validation IDs for the Operator
func (o *operator) GetClusterValidationIDs() []string {
	return []string{clusterValidationID}
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return hostValidationID
}

// ValidateCluster always return "valid" result
func (o *operator) ValidateCluster(_ context.Context, cluster *common.Cluster) ([]api.ValidationResult, error) {
	result := []api.ValidationResult{{
		Status:       api.Success,
		ValidationId: clusterValidationID,
	}}

	if ok, _ := common.BaseVersionLessThan(LvmoMinOpenshiftVersion, cluster.OpenshiftVersion); ok {
		result[0].Status = api.Failure
		result[0].Reasons = []string{
			fmt.Sprintf("Logical Volume Manager is only supported for openshift versions %s and above", LvmoMinOpenshiftVersion),
		}

		return result, nil
	}

	if !common.IsSingleNodeCluster(cluster) {
		if ok, _ := common.BaseVersionLessThan(LvmMinMultiNodeSupportVersion, cluster.OpenshiftVersion); ok {
			result[0].Status = api.Failure
			result[0].Reasons = []string{
				fmt.Sprintf("Logical Volume Manager is only supported for highly available openshift with version %s or above", LvmMinMultiNodeSupportVersion),
			}

			return result, nil
		}
	}

	return result, nil
}

// ValidateHost always return "valid" result
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host, additionalOperatorRequirements *models.ClusterHostRequirementsDetails) (api.ValidationResult, error) {
	if host.Inventory == "" {
		message := "Missing Inventory in the host"
		return api.ValidationResult{Status: api.Pending, ValidationId: o.GetHostValidationID(), Reasons: []string{message}}, nil
	}

	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		message := "Failed to get inventory from host"
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message}}, err
	}

	minDiskSizeGb := int64(0)
	if additionalOperatorRequirements != nil {
		minDiskSizeGb = additionalOperatorRequirements.DiskSizeGb
	}
	diskCount, _ := operatorscommon.NonInstallationDiskCount(inventory.Disks, host.InstallationDiskID, minDiskSizeGb)

	role := common.GetEffectiveRole(host)
	areSchedulable := common.ShouldMastersBeSchedulable(&cluster.Cluster)
	minSizeMessage := ""
	if minDiskSizeGb > 0 {
		minSizeMessage = fmt.Sprintf(" of %dGB minimum", minDiskSizeGb)
	}
	message := fmt.Sprintf("Logical Volume Manager requires at least one non-installation HDD/SSD disk%s on the host", minSizeMessage)

	if role == models.HostRoleWorker || (areSchedulable && role != models.HostRoleArbiter) {
		if diskCount == 0 {
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message}}, nil
		}
	} else {
		if role == models.HostRoleAutoAssign {
			status := "For Logical Volume Manager Standard Mode, host role must be assigned to master or worker."
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{status}}, nil
		}
	}
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
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	log := logutil.FromContext(ctx, o.log)
	preflightRequirements, err := o.GetPreflightRequirements(ctx, cluster)
	if err != nil {
		log.WithError(err).Errorf("Cannot retrieve preflight requirements for host %s", host.ID)
		return nil, err
	}

	role := common.GetEffectiveRole(host)
	areSchedulable := common.ShouldMastersBeSchedulable(&cluster.Cluster)

	if (role == models.HostRoleMaster && !areSchedulable) || role == models.HostRoleArbiter {
		return &models.ClusterHostRequirementsDetails{
			CPUCores: 0,
			RAMMib:   0,
		}, nil
	}

	return preflightRequirements.Requirements.Master.Quantitative, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(context context.Context, cluster *common.Cluster) (*models.OperatorHardwareRequirements, error) {
	dependecies, err := o.GetDependencies(cluster)
	if err != nil {
		return &models.OperatorHardwareRequirements{}, err
	}

	memoryRequirements := o.Config.LvmMemoryPerHostMiB
	if ok, _ := common.BaseVersionLessThan(LvmsMinOpenshiftVersion_ForNewResourceRequirements, cluster.OpenshiftVersion); ok {
		memoryRequirements = o.Config.LvmMemoryPerHostMiBBefore4_13
	}

	if ok, _ := common.BaseVersionGreaterOrEqual(LvmNewResourcesOpenshiftVersion4_16, cluster.OpenshiftVersion); ok {
		memoryRequirements = o.Config.LvmMemoryPerHostMiBFrom4_16
	}
	requirementMessage := []string{
		"At least 1 non-boot disk per host",
		fmt.Sprintf("%d MiB of additional RAM", memoryRequirements),
		fmt.Sprintf("%d additional CPUs for each non-boot disk", o.Config.LvmCPUPerHost),
	}

	return &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: dependecies,
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Qualitative: requirementMessage,
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.Config.LvmCPUPerHost,
					RAMMib:   memoryRequirements,
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Qualitative: requirementMessage,
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.Config.LvmCPUPerHost,
					RAMMib:   memoryRequirements,
				},
			},
		},
	}, nil
}

func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDLVM
}

// GetBundleLabels returns the bundle labels for the LSO operator
func (o *operator) GetBundleLabels() []string {
	return []string(Operator.Bundles)
}
