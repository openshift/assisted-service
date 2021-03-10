package cnv

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/lso"
	models "github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

const (
	// Memory value provided in MiB
	masterMemory int64 = 150
	masterCPU    int64 = 4
	// Memory value provided in MiB
	workerMemory int64 = 360
	workerCPU    int64 = 2
)

type operator struct {
	log logrus.FieldLogger
}

var Operator = models.MonitoredOperator{
	Name:           "cnv",
	OperatorType:   models.OperatorTypeOlm,
	TimeoutSeconds: 60 * 60,
}

// NewCNVOperator creates new instance of a Container Native Virtualization installation plugin
func NewCNVOperator(log logrus.FieldLogger) *operator {
	return &operator{
		log: log,
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
	return string(models.ClusterValidationIDCnvRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDCnvRequirementsSatisfied)
}

// ValidateCluster always return "valid" result
func (o *operator) ValidateCluster(ctx context.Context, cluster *common.Cluster) (api.ValidationResult, error) {
	// No need to validate cluster because it will be validate on per host basis
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetClusterValidationID()}, nil
}

// ValidateHost returns validationResult based on node type requirements such as memory and cpu
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host) (api.ValidationResult, error) {
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		o.log.Errorf("Failed to get inventory from host with id %s", host.ID)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID()}, err
	}
	// TODO: validate available devices on worker node like gpu and sr-iov and check whether there is enough memory to support them
	if host.Role == models.HostRoleWorker {
		cpu, _ := o.GetCPURequirementForWorker(ctx, cluster)
		if inventory.CPU.Count < cpu {
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{fmt.Sprintf("Insufficient CPU to deploy CNV. Required CPU count is %d but found %d ", cpu, inventory.CPU.Count)}}, nil
		}
		mem, _ := o.GetMemoryRequirementForWorker(ctx, cluster)
		if inventory.Memory.UsableBytes < mem {
			usableMemory := hardware.BytesToMib(inventory.Memory.UsableBytes)
			memBytes := hardware.BytesToMib(mem)
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{fmt.Sprintf("Insufficient memory to deploy CNV. Required memory is %d MiB but found %d MiB", memBytes, usableMemory)}}, nil
		}
	} else if host.Role == models.HostRoleMaster {
		cpu, _ := o.GetCPURequirementForMaster(ctx, cluster)
		if inventory.CPU.Count < cpu {
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{fmt.Sprintf("Insufficient CPU to deploy CNV. Required CPU count is %d but found %d ", cpu, inventory.CPU.Count)}}, nil
		}
		mem, _ := o.GetMemoryRequirementForMaster(ctx, cluster)
		if inventory.Memory.UsableBytes < mem {
			usableMemory := hardware.BytesToMib(inventory.Memory.UsableBytes)
			memBytes := hardware.BytesToMib(mem)
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetClusterValidationID(), Reasons: []string{fmt.Sprintf("Insufficient memory to deploy CNV. Required memory is %d MiB but found %d MiB", memBytes, usableMemory)}}, nil
		}
	}
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetClusterValidationID()}, nil
}

// GetCPURequirementForWorker provides worker CPU requirements for the operator
func (o *operator) GetCPURequirementForWorker(context.Context, *common.Cluster) (int64, error) {
	return workerCPU, nil
}

// GetCPURequirementForMaster provides master CPU requirements for the operator
func (o *operator) GetCPURequirementForMaster(context.Context, *common.Cluster) (int64, error) {
	return masterCPU, nil
}

// GetMemoryRequirementForWorker provides worker memory requirements for the operator
func (o *operator) GetMemoryRequirementForWorker(ctx context.Context, cluster *common.Cluster) (int64, error) {
	return hardware.MibToBytes(workerMemory), nil
}

// GetMemoryRequirementForMaster provides master memory requirements for the operator
func (o *operator) GetMemoryRequirementForMaster(ctx context.Context, cluster *common.Cluster) (int64, error) {
	return hardware.MibToBytes(masterMemory), nil
}

// GenerateManifests generates manifests for the operator
func (o *operator) GenerateManifests(c *common.Cluster) (map[string][]byte, error) {
	return Manifests(c.Cluster.OpenshiftVersion)
}

// GetDisksRequirementForMaster provides a number of disks required in a master
func (o *operator) GetDisksRequirementForMaster(context.Context, *common.Cluster) (int64, error) {
	return 0, nil
}

// GetDisksRequirementForWorker provides a number of disks required in a worker
func (o *operator) GetDisksRequirementForWorker(context.Context, *common.Cluster) (int64, error) {
	return 0, nil
}

// GetProperties provides description of operator properties: none required
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to the CNV Operator
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}
