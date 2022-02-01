package ocs

import (
	"context"
	"fmt"
	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
)

const ocsLabel string = "cluster.ocs.openshift.io/openshift-storage"

// operator is an OCS OLM operator plugin; it implements api.Operator
type operator struct {
	log       logrus.FieldLogger
	config    *Config
	extracter oc.Extracter
}

var Operator = models.MonitoredOperator{
	Name:             "ocs",
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        "openshift-storage",
	SubscriptionName: "ocs-operator",
	TimeoutSeconds:   30 * 60,
}

// NewOcsOperator creates new OCSOperator
func NewOcsOperator(log logrus.FieldLogger, extracter oc.Extracter) *operator {
	cfg := Config{}
	err := envconfig.Process(common.EnvConfigPrefix, &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return newOcsOperatorWithConfig(log, &cfg, extracter)
}

// newOcsOperatorWithConfig creates new OCSOperator with given configuration
func newOcsOperatorWithConfig(log logrus.FieldLogger, config *Config, extracter oc.Extracter) *operator {
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
func (o *operator) ValidateHost(_ context.Context, cluster *common.Cluster, host *models.Host) (api.ValidationResult, error) {
	//ocsLabel := "node.ocs.openshift.io/storage"
	numOfHosts := int64(len(cluster.Hosts))

	if host.Inventory == "" {
		return api.ValidationResult{Status: api.Pending, ValidationId: o.GetHostValidationID(), Reasons: []string{"Missing Inventory in host"}}, nil
	}
	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		message := "Failed to get inventory from host"
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message}}, err
	}

	// GetValidDiskCount counts the total number of valid disks in each host and return a error if we don't have the disk of required size
	diskCount, err := o.getValidDiskCount(inventory.Disks, host.InstallationDiskID)
	if err != nil {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{err.Error()}}, nil
	}

	role := common.GetEffectiveRole(host)
	label := host.Labels

	if numOfHosts <= o.config.OCSNumMinimumHosts { // compact mode
		if role == models.HostRoleMaster || role == models.HostRoleAutoAssign {
			if diskCount == 0 {
				return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{"In compact mode, OCS requires at least one non-bootable disk on each labeled host"}}, nil
			} else { // diskCount > 0
				if label == ocsLabel {
					return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID(), Reasons: []string{}}, nil
				} else { // label != ocsLabel
					return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID(), Reasons: []string{"Host not selected for OCS"}}, nil
				}
			}
		} else { // role == models.HostRoleWorker
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{"In compact mode, host role must be master or auto-assign"}}, nil
		}
	} else { // standard mode
		if role == models.HostRoleAutoAssign {
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{"In standard mode, host role must be master or worker"}}, nil
		} else { // role == models.HostRoleMaster || role == models.HostRoleWorker
			if label == ocsLabel {
				return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID(), Reasons: []string{}}, nil
			} else {
				return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID(), Reasons: []string{"Host not selected for OCS"}}, nil
			}
		}
	}
}

// GenerateManifests generates manifests for the operator
func (o *operator) GenerateManifests(cluster *common.Cluster) (map[string][]byte, []byte, error) {
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
	//ocsLabel := "node.ocs.openshift.io/storage"
	numOfHosts := int64(len(cluster.Hosts))

	var diskCount int64 = 0
	if host.Inventory != "" {
		inventory, err := common.UnmarshalInventory(host.Inventory)
		if err != nil {
			return nil, err
		}

		/* GetValidDiskCount counts the total number of valid disks in each host and return a error if we don't have the disk of required size,
		we ignore the error as its treated as 500 in the UI */
		diskCount, _ = o.getValidDiskCount(inventory.Disks, host.InstallationDiskID)
	}

	role := common.GetEffectiveRole(host)
	label := host.Labels

	if numOfHosts <= o.config.OCSNumMinimumHosts { // Compact mode
		var reqDisks int64 = 1
		if diskCount > 0 {
			reqDisks = diskCount
		}
		if label == ocsLabel && (role == models.HostRoleMaster || role == models.HostRoleAutoAssign) {
			return &models.ClusterHostRequirementsDetails{
				CPUCores: o.config.OCSPerHostCPUCompactMode + (reqDisks * o.config.OCSPerDiskCPUCount),
				RAMMib:   conversions.GibToMib(o.config.OCSPerHostMemoryGiBCompactMode + (reqDisks * o.config.OCSPerDiskRAMGiB)),
			}, nil
		} else if label == ocsLabel && role == models.HostRoleWorker {
			return &models.ClusterHostRequirementsDetails{
				CPUCores: o.config.OCSPerHostCPUStandardMode + (reqDisks * o.config.OCSPerDiskCPUCount),
				RAMMib:   conversions.GibToMib(o.config.OCSPerHostMemoryGiBStandardMode + (reqDisks * o.config.OCSPerDiskRAMGiB)),
			}, nil
		} else { // label != ocsLabel
			return &models.ClusterHostRequirementsDetails{CPUCores: 0, RAMMib: 0}, nil
		}
	} else { // Standard mode
		if label != ocsLabel || role == models.HostRoleMaster { // In standard mode, OCS does not run on master nodes so return zero
			return &models.ClusterHostRequirementsDetails{CPUCores: 0, RAMMib: 0}, nil
		} else { // label == ocsLabel && (role == models.HostRoleWorker || role == models.HostRoleAutoAssign)
			return &models.ClusterHostRequirementsDetails{
				CPUCores: o.config.OCSPerHostCPUStandardMode + (diskCount * o.config.OCSPerDiskCPUCount),
				RAMMib:   conversions.GibToMib(o.config.OCSPerHostMemoryGiBStandardMode + (diskCount * o.config.OCSPerDiskRAMGiB)),
			}, nil
		}
	}

	//// worker and auto-assign
	//if diskCount > 0 {
	//	// for each disk ocs requires 2 CPUs and 5 GiB RAM
	//	return &models.ClusterHostRequirementsDetails{
	//		CPUCores: o.config.OCSPerHostCPUStandardMode + (diskCount * o.config.OCSPerDiskCPUCount),
	//		RAMMib:   conversions.GibToMib(o.config.OCSPerHostMemoryGiBStandardMode + (diskCount * o.config.OCSPerDiskRAMGiB)),
	//	}, nil
	//}
	//return &models.ClusterHostRequirementsDetails{
	//	CPUCores: o.config.OCSPerHostCPUStandardMode,
	//	RAMMib:   conversions.GibToMib(o.config.OCSPerHostMemoryGiBStandardMode),
	//}, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(context.Context, *common.Cluster) (*models.OperatorHardwareRequirements, error) {
	return &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: o.GetDependencies(),
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.config.OCSPerHostCPUCompactMode,
					RAMMib:   conversions.GibToMib(o.config.OCSPerHostMemoryGiBCompactMode),
				},
				Qualitative: []string{
					"Requirements apply only for master-only clusters",
					"At least 3 hosts",
					"At least 1 non-boot disk on 3 hosts",
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: o.config.OCSPerHostCPUStandardMode,
					RAMMib:   conversions.GibToMib(o.config.OCSPerHostMemoryGiBStandardMode),
				},
				Qualitative: []string{
					"Requirements apply only for clusters with workers",
					fmt.Sprintf("%v GiB of additional RAM for each non-boot disk", o.config.OCSPerDiskRAMGiB),
					fmt.Sprintf("%v additional CPUs for each non-boot disk", o.config.OCSPerDiskCPUCount),
					"At least 3 workers",
					"At least 1 non-boot disk on 3 workers",
				},
			},
		},
	}, nil
}
