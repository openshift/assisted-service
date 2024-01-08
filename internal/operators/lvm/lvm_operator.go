package lvm

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-version"
	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

// operator is an ODF LVM OLM operator plugin; it implements api.Operator
type operator struct {
	log       logrus.FieldLogger
	config    *Config
	extracter oc.Extracter
}

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
		config:    config,
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
	ocpVersion, err := version.NewVersion(cluster.OpenshiftVersion)
	if err != nil {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{err.Error()}}, nil
	}

	if common.IsSingleNodeCluster(cluster) {
		minOpenshiftVersionForLvm, err := version.NewVersion(o.config.LvmMinOpenshiftVersion)
		if err != nil {
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{err.Error()}}, nil
		}
		if ok, _ := common.BaseVersionLessThan(minOpenshiftVersionForLvm.String(), ocpVersion.String()); ok {
			message := fmt.Sprintf("Logical Volume Manager is only supported for openshift versions %s and above", o.config.LvmMinOpenshiftVersion)
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{message}}, nil
		}
	} else {
		// HA support was introduced after LVM support in general, so we need to check for a different version
		minOpenshiftVersionForMultiNodeSupport, err := version.NewVersion(o.config.LvmMinMultiNodeSupportVersion)
		if err != nil {
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{err.Error()}}, nil
		}
		if ok, _ := common.BaseVersionLessThan(minOpenshiftVersionForMultiNodeSupport.String(), ocpVersion.String()); ok {
			message := fmt.Sprintf("Logical Volume Manager is only supported for highly available openshift with version %s or above",
				minOpenshiftVersionForMultiNodeSupport.String())
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{message}}, nil
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

	// GetValidDiskCount counts the total number of valid disks in each host and return an error if we don't have the disk of required size
	diskCount := o.getValidDiskCount(inventory.Disks, host.InstallationDiskID)
	if err != nil {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{err.Error()}}, nil
	}
	if diskCount == 0 {
		message := "Logical Volume Manager requires at least one non-installation HDD/SSD disk on the host"
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message}}, nil
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
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.config.LvmCPUPerHost,
					RAMMib:   o.getLvmMemoryPerHostMib(context, cluster),
				},
				Qualitative: []string{
					"At least 1 non-installation disk with no partitions or filesystems",
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

func (o *operator) getLvmMemoryPerHostMib(ctx context.Context, cluster *common.Cluster) int64 {
	log := logutil.FromContext(ctx, o.log)
	greaterOrEqual, err := common.BaseVersionGreaterOrEqual(LvmsMinOpenshiftVersionForNewResourceRequirements, cluster.OpenshiftVersion)
	if err != nil {
		log.Warnf("Error parsing cluster.OpenshiftVersion: %s, setting lvms memory requirement to %d", err.Error(), o.config.LvmMemoryPerHostMiB)
		return o.config.LvmMemoryPerHostMiB
	}
	if !greaterOrEqual {
		return LvmsMemoryRequirementBefore4_13
	}

	return o.config.LvmMemoryPerHostMiB
}
