package lvm

import (
	"context"
	"fmt"

	"github.com/go-openapi/swag"
	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/operators/api"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

// operator is an ODF LVM OLM operator plugin; it implements api.Operator
type operator struct {
	log                         logrus.FieldLogger
	Config                      *Config
	extracter                   oc.Extracter
	additionalDiskRequirementGB int64
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
func NewLvmOperator(log logrus.FieldLogger, extracter oc.Extracter) *operator {
	cfg := Config{}
	err := envconfig.Process(common.EnvConfigPrefix, &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return newLvmOperatorWithConfig(log, &cfg, extracter)
}

// newOdfOperatorWithConfig creates new ODFOperator with given configuration
func newLvmOperatorWithConfig(log logrus.FieldLogger, config *Config, extracter oc.Extracter) *operator {
	return &operator{
		log:       log,
		Config:    config,
		extracter: extracter,
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

func (o *operator) SetAdditionalDiskRequirements(additionalSizeGB int64) {
	o.additionalDiskRequirementGB = additionalSizeGB
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	return make([]string, 0), nil
}

// GetClusterValidationID returns cluster validation ID for the Operator
func (o *operator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDLvmRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDLvmRequirementsSatisfied)
}

// ValidateCluster always return "valid" result
func (o *operator) ValidateCluster(_ context.Context, cluster *common.Cluster) (api.ValidationResult, error) {
	if ok, _ := common.BaseVersionLessThan(LvmoMinOpenshiftVersion, cluster.OpenshiftVersion); ok {
		message := fmt.Sprintf("Logical Volume Manager is only supported for openshift versions %s and above", LvmoMinOpenshiftVersion)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{message}}, nil
	}

	if swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeFull {
		if ok, _ := common.BaseVersionLessThan(LvmMinMultiNodeSupportVersion, cluster.OpenshiftVersion); ok {
			message := fmt.Sprintf("Logical Volume Manager is only supported for highly available openshift with version %s or above", LvmMinMultiNodeSupportVersion)
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message}}, nil
		}
	}
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetClusterValidationID()}, nil
}

// ValidateHost always return "valid" result
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host) (api.ValidationResult, error) {
	if host.Inventory == "" {
		message := "Missing Inventory in the host"
		return api.ValidationResult{Status: api.Pending, ValidationId: o.GetHostValidationID(), Reasons: []string{message}}, nil
	}

	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		message := "Failed to get inventory from host"
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message}}, err
	}

	diskCount, _ := operatorscommon.NonInstallationDiskCount(inventory.Disks, host.InstallationDiskID, o.additionalDiskRequirementGB)

	role := common.GetEffectiveRole(host)
	areSchedulable := common.AreMastersSchedulable(cluster)
	minSizeMessage := ""
	if o.additionalDiskRequirementGB > 0 {
		minSizeMessage = fmt.Sprintf(" of %dGB minimum", o.additionalDiskRequirementGB)
	}
	message := fmt.Sprintf("Logical Volume Manager requires at least one non-installation HDD/SSD disk%s on the host", minSizeMessage)

	if role == models.HostRoleWorker || areSchedulable {
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
	if role == models.HostRoleMaster && !swag.BoolValue(cluster.SchedulableMasters) {
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

	return &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: dependecies,
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Qualitative: []string{
					"At least 1 non-boot disk per host",
					fmt.Sprintf("%d MiB of additional RAM", memoryRequirements),
					fmt.Sprintf("%d additional CPUs for each non-boot disk", o.Config.LvmCPUPerHost),
				},
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.Config.LvmCPUPerHost,
					RAMMib:   memoryRequirements,
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{},
			},
		},
	}, nil
}

func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDLVM
}
