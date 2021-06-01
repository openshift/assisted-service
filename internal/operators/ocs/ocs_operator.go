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
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host) (api.ValidationResult, error) {
	numOfHosts := len(cluster.Hosts)
	if host.Inventory == "" {
		o.log.Info("Empty Inventory of host with hostID ", host.ID)
		return api.ValidationResult{Status: api.Pending, ValidationId: o.GetHostValidationID(), Reasons: []string{"Missing Inventory in some of the hosts"}}, nil
	}
	inventory, err := hostutil.UnmarshalInventory(host.Inventory)
	if err != nil {
		o.log.Errorf("Failed to get inventory from host with id %s", host.ID)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID()}, err
	}

	/* GetValidDiskCount counts the total number of valid disks in each host and return a error if we don't have the disk of required size,
	we ignore the number of valid Disks as its handled later based on mode of deployment  */
	_, err = getValidDiskCount(inventory.Disks, host.InstallationDiskID)
	if err != nil {
		o.log.Errorf("%s %s", err.Error(), host.ID)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{err.Error()}}, nil
	}

	// compact mode
	if numOfHosts <= 3 {
		if host.Role == models.HostRoleMaster || host.Role == models.HostRoleAutoAssign {
			return o.checkHostRequirements(ctx, cluster, host, inventory, compactMode)
		}
		o.log.Errorf("OCS unsupported Host Role for Compact Mode")
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{"OCS unsupported Host Role for Compact Mode."}}, nil
	}

	// Minimal and Standard mode
	// If the Role is set to Auto-assign for a host, it is not possible to determine whether the node will end up as a master or worker node.
	if host.Role == models.HostRoleAutoAssign {
		status := "All host roles must be assigned to enable OCS in Standard or Minimal Mode."
		o.log.Info("OCS Validate Requirements status ", status)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{status}}, nil
	}
	return o.checkHostRequirements(ctx, cluster, host, inventory, minimalMode)
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

	var diskCount int64 = 0
	if host.Inventory != "" {
		inventory, err := hostutil.UnmarshalInventory(host.Inventory)
		if err != nil {
			return nil, err
		}

		/* GetValidDiskCount counts the total number of valid disks in each host and return a error if we don't have the disk of required size,
		we ignore the error as its treated as 500 in the UI */
		diskCount, _ = getValidDiskCount(inventory.Disks, host.InstallationDiskID)
	}

	if numOfHosts <= 3 { // Compact Mode
		var reqDisks int64 = 1
		if diskCount > 0 {
			reqDisks = diskCount
		}
		// for each disk ocs requires 2 CPUs and 5 GiB RAM
		if host.Role == models.HostRoleMaster || host.Role == models.HostRoleAutoAssign {
			return &models.ClusterHostRequirementsDetails{
				CPUCores:   CPUCompactMode + reqDisks*o.config.OCSRequiredDiskCPUCount,
				RAMMib:     conversions.GibToMib(MemoryGiBCompactMode + reqDisks*o.config.OCSRequiredDiskRAMGiB),
				DiskSizeGb: ocsMinDiskSize,
			}, nil
		}
		// regular worker req
		return &models.ClusterHostRequirementsDetails{
			CPUCores:   CPUMinimalMode + reqDisks*o.config.OCSRequiredDiskCPUCount,
			RAMMib:     conversions.GibToMib(MemoryGiBMinimalMode + reqDisks*o.config.OCSRequiredDiskRAMGiB),
			DiskSizeGb: ocsMinDiskSize,
		}, nil
	}

	// The OCS minimal deployment mode sets up a storage cluster using reduced resources. If the resources will be more, standard mode will be deployed automatically.
	// In minimum and standard mode, OCS does not run on master nodes so return zero
	if host.Role == models.HostRoleMaster {
		return &models.ClusterHostRequirementsDetails{CPUCores: 0, RAMMib: 0}, nil
	}

	// worker and auto-assign
	if diskCount > 0 {
		// for each disk ocs requires 2 CPUs and 5 GiB RAM
		return &models.ClusterHostRequirementsDetails{
			CPUCores:   CPUMinimalMode + diskCount*o.config.OCSRequiredDiskCPUCount,
			RAMMib:     conversions.GibToMib(MemoryGiBMinimalMode + diskCount*o.config.OCSRequiredDiskRAMGiB),
			DiskSizeGb: ocsMinDiskSize,
		}, nil
	}
	return &models.ClusterHostRequirementsDetails{
		CPUCores: CPUMinimalMode,
		RAMMib:   conversions.GibToMib(MemoryGiBMinimalMode),
	}, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(context.Context, *common.Cluster) (*models.OperatorHardwareRequirements, error) {
	return &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: o.GetDependencies(),
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: CPUCompactMode,
					RAMMib:   conversions.GibToMib(MemoryGiBCompactMode),
				},
				Qualitative: []string{
					"Requirements apply only for master-only clusters",
					"At least 3 hosts",
					"At least 1 non-boot disk on 3 hosts",
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: CPUMinimalMode,
					RAMMib:   conversions.GibToMib(MemoryGiBMinimalMode),
				},
				Qualitative: []string{
					"Requirements apply only for clusters with workers",
					fmt.Sprintf("%v GiB of additional RAM for each non-boot disk", o.config.OCSRequiredDiskRAMGiB),
					fmt.Sprintf("%v additional CPUs for each non-boot disk", o.config.OCSRequiredDiskCPUCount),
					"At least 3 workers",
					"At least 1 non-boot disk on 3 workers",
				},
			},
		},
	}, nil
}

func (o *operator) checkHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host, inventory *models.Inventory, mode ocsDeploymentMode) (api.ValidationResult, error) {
	requirements, err := o.GetHostRequirements(ctx, cluster, host)
	if err != nil {
		message := fmt.Sprintf("Failed to get host requirements for host with id %s", host.ID)
		o.log.Error(message)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message, err.Error()}}, err
	}

	if inventory.CPU.Count < requirements.CPUCores {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{fmt.Sprintf("Insufficient CPU to deploy OCS. Required CPU count is %d but found %d.", requirements.CPUCores, inventory.CPU.Count)}}, nil
	}

	if inventory.Memory.UsableBytes < conversions.MibToBytes(requirements.RAMMib) {
		usableMemory := conversions.BytesToGiB(inventory.Memory.UsableBytes)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{fmt.Sprintf("Insufficient memory to deploy OCS. Required memory is %d GiB but found %d GiB.", conversions.MibToGiB(requirements.RAMMib), usableMemory)}}, nil
	}

	if diskCount, _ := getValidDiskCount(inventory.Disks, host.InstallationDiskID); diskCount <= 0 && mode == compactMode {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{"Insufficient disk to deploy OCS. OCS requires to have at least one non-bootable on each host in compact mode."}}, nil
	}

	return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID(), Reasons: []string{}}, nil
}
