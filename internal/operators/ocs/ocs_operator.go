package ocs

import (
	"context"
	"fmt"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
)

// operator is an OCS OLM operator plugin; it implements api.Operator
type operator struct {
	log    logrus.FieldLogger
	config *Config
}

var Operator = models.MonitoredOperator{
	Name:             "ocs",
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        "openshift-storage",
	SubscriptionName: "ocs-operator",
	TimeoutSeconds:   30 * 60,
}

// NewOcsOperator creates new OCSOperator
func NewOcsOperator(log logrus.FieldLogger) *operator {
	cfg := Config{}
	err := envconfig.Process(common.EnvConfigPrefix, &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return newOcsOperatorWithConfig(log, &cfg)
}

// newOcsOperatorWithConfig creates new OCSOperator with given configuration
func newOcsOperatorWithConfig(log logrus.FieldLogger, config *Config) *operator {
	return &operator{
		log:    log,
		config: config,
	}
}

// GetName reports the name of an operator this Operator manages
func (o *operator) GetName() string {
	return Operator.Name
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies() []string {
	return []string{lso.Operator.Name}
}

// GetClusterValidationID returns cluster validation ID for the Operator
func (o *operator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDOcsRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDOcsRequirementsSatisfied)
}

// ValidateCluster verifies whether this operator is valid for given cluster
func (o *operator) ValidateCluster(_ context.Context, cluster *common.Cluster) (api.ValidationResult, error) {
	status, message := o.validateRequirements(&cluster.Cluster)

	return api.ValidationResult{Status: status, ValidationId: o.GetClusterValidationID(), Reasons: []string{message}}, nil
}

// ValidateHost verifies whether this operator is valid for given host
func (o *operator) ValidateHost(_ context.Context, _ *common.Cluster, _ *models.Host) (api.ValidationResult, error) {
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID(), Reasons: []string{}}, nil
}

// GenerateManifests generates manifests for the operator
func (o *operator) GenerateManifests(cluster *common.Cluster) (map[string][]byte, error) {
	o.log.Info("No. of OCS eligible disks are ", o.config.OCSDisksAvailable)
	return Manifests(o.config)
}

// GetProperties provides description of operator properties: none required
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to the OCS Operator
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GetHostRequirements provides operator's requirements towards the host
func (o *operator) GetHostRequirements(_ context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	numOfHosts := len(cluster.Hosts)

	inventory, err := hostutil.UnmarshalInventory(host.Inventory)
	if err != nil {
		return nil, err
	}

	disks := int64(getValidDiskCount(inventory.Disks) - 1)

	if numOfHosts < 3 {
		return nil, fmt.Errorf("OCS requires a minimum of 3 hosts")
	}

	if numOfHosts == 3 { // Compact Mode
		if host.Role == models.HostRoleMaster || host.Role == models.HostRoleAutoAssign {
			if disks <= 0 {
				return nil, fmt.Errorf("OCS requires a minimum of one non-bootable disk per host")
			}
			return &models.ClusterHostRequirementsDetails{
				CPUCores:   CPUCompactMode + disks*o.config.OCSRequiredDiskCPUCount,
				RAMMib:     conversions.GbToMib(MemoryGBCompactMode + disks*o.config.OCSRequiredDiskRAMGB),
				DiskSizeGb: MinDiskSize,
			}, nil
		}
		return nil, fmt.Errorf("OCS compact mode unsupported role: %s", host.Role)
	}

	if host.Role == models.HostRoleAutoAssign { //If auto-assign we cannot proceed with minimal or standard mode
		return nil, fmt.Errorf("OCS minimal mode unsupported role: %s", host.Role)
	}

	// The OCS minimal deployment mode sets up a storage cluster using reduced resources. If the resources will be more, standard mode will be deployed automatically.
	// In minimum and standard mode, OCS does not run on master nodes so return zero
	if host.Role == models.HostRoleMaster {
		return &models.ClusterHostRequirementsDetails{CPUCores: 0, RAMMib: 0}, nil
	}

	if disks > 0 {
		// for each disk ocs requires 2 CPUs and 5 GB RAM
		return &models.ClusterHostRequirementsDetails{
			CPUCores:   CPUMinimalMode + disks*o.config.OCSRequiredDiskCPUCount,
			RAMMib:     conversions.GbToMib(MemoryGBMinimalMode + disks*o.config.OCSRequiredDiskRAMGB),
			DiskSizeGb: MinDiskSize,
		}, nil
	}
	return &models.ClusterHostRequirementsDetails{
		CPUCores: CPUMinimalMode,
		RAMMib:   conversions.GbToMib(MemoryGBMinimalMode),
	}, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(context.Context, *common.Cluster) (*models.OperatorHardwareRequirements, error) {
	return &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: o.GetDependencies(),
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				// TODO: adjust when https://github.com/openshift/assisted-service/pull/1456 is merged
				Quantitative: &models.ClusterHostRequirementsDetails{},
				Qualitative: []string{
					"At least 3 hosts in case of masters-only cluster",
					"At least 1 non-boot disk on 3 hosts",
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				// TODO: adjust when https://github.com/openshift/assisted-service/pull/1456 is merged
				Quantitative: &models.ClusterHostRequirementsDetails{},
				Qualitative: []string{
					fmt.Sprintf("%v GiB of additional RAM for each non-boot disk", o.config.OCSRequiredDiskRAMGB),
					fmt.Sprintf("%v additional CPUs for each non-boot disk", o.config.OCSRequiredDiskCPUCount),
					"At least 3 hosts in case of cluster with workers",
					"At least 1 non-boot disk on 3 hosts",
				},
			},
		},
	}, nil
}
