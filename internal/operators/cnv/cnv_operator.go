package cnv

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/hardware/virt"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

const (
	// Memory value provided in MiB
	MasterMemory int64 = 150
	MasterCPU    int64 = 4
	// Memory value provided in MiB
	WorkerMemory int64 = 360
	WorkerCPU    int64 = 2
)

type operator struct {
	log    logrus.FieldLogger
	config Config
}

var Operator = models.MonitoredOperator{
	Name:             "cnv",
	Namespace:        DownstreamNamespace,
	OperatorType:     models.OperatorTypeOlm,
	SubscriptionName: "hco-operatorhub",
	TimeoutSeconds:   60 * 60,
}

// NewCNVOperator creates new instance of a Container Native Virtualization installation plugin
func NewCNVOperator(log logrus.FieldLogger, cfg Config) *operator {
	log.WithField("config", cfg).Infof("Configuring CNV Operator plugin")
	return &operator{
		log:    log,
		config: cfg,
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
func (o *operator) ValidateCluster(_ context.Context, _ *common.Cluster) (api.ValidationResult, error) {
	// No need to validate cluster because it will be validate on per host basis
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetClusterValidationID()}, nil
}

// ValidateHost returns validationResult based on node type requirements such as memory and cpu
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host) (api.ValidationResult, error) {
	if host.Inventory == "" {
		o.log.Info("Empty Inventory of host with hostID ", host.ID)
		return api.ValidationResult{Status: api.Pending, ValidationId: o.GetHostValidationID(), Reasons: []string{"Missing Inventory in some of the hosts"}}, nil
	}
	inventory, err := hostutil.UnmarshalInventory(host.Inventory)
	if err != nil {
		o.log.Errorf("Failed to get inventory from host with id %s", host.ID)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID()}, err
	}

	if !virt.IsVirtSupported(inventory) {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{"CPU does not have virtualization support "}}, nil
	}

	// If the Role is set to Auto-assign for a host, it is not possible to determine whether the node will end up as a master or worker node.
	if host.Role == models.HostRoleAutoAssign {
		status := "All host roles must be assigned to enable CNV."
		o.log.Info("Validate Requirements status ", status)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{status}}, nil
	}
	requirements, err := o.GetHostRequirements(ctx, cluster, host)
	if err != nil {
		message := fmt.Sprintf("Failed to get host requirements for host with id %s", host.ID)
		o.log.Error(message)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message, err.Error()}}, err
	}

	cpu := requirements.CPUCores
	if inventory.CPU.Count < cpu {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{fmt.Sprintf("Insufficient CPU to deploy CNV. Required CPU count is %d but found %d ", cpu, inventory.CPU.Count)}}, nil
	}

	mem := requirements.RAMMib
	memBytes := conversions.MibToBytes(mem)
	if inventory.Memory.UsableBytes < memBytes {
		usableMemory := conversions.BytesToMib(inventory.Memory.UsableBytes)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{fmt.Sprintf("Insufficient memory to deploy CNV. Required memory is %d MiB but found %d MiB", mem, usableMemory)}}, nil
	}

	// TODO: validate available devices on worker node like gpu and sr-iov and check whether there is enough memory to support them
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID()}, nil
}

// GenerateManifests generates manifests for the operator
func (o *operator) GenerateManifests(c *common.Cluster) (map[string][]byte, error) {
	return Manifests(o.config)
}

// GetProperties provides description of operator properties: none required
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to the CNV Operator
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	opt := Operator
	if !o.config.Mode {
		opt.Namespace = UpstreamNamespace
	}
	return &opt
}

// GetHostRequirements provides operator's requirements towards the host
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	log := logutil.FromContext(ctx, o.log)
	preflightRequirements, err := o.GetPreflightRequirements(ctx, cluster)
	if err != nil {
		log.WithError(err).Errorf("Cannot Retrieve prefligh requirements for cluster %s", cluster.ID)
		return nil, err
	}

	switch host.Role {
	case models.HostRoleMaster:
		return preflightRequirements.Requirements.Master.Quantitative, nil
	case models.HostRoleWorker, models.HostRoleAutoAssign:
		overhead, err := o.getDevicesMemoryOverhead(host)
		if err != nil {
			log.WithError(err).WithField("inventory", host.Inventory).Errorf("Cannot parse inventory for host %v", host.ID)
			return nil, err
		}
		workerBaseRequirements := preflightRequirements.Requirements.Worker.Quantitative
		return &models.ClusterHostRequirementsDetails{
			CPUCores: workerBaseRequirements.CPUCores,
			RAMMib:   workerBaseRequirements.RAMMib + overhead,
		}, nil
	}
	return nil, fmt.Errorf("unsupported role: %s", host.Role)
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(context.Context, *common.Cluster) (*models.OperatorHardwareRequirements, error) {
	qualitativeRequirements := []string{
		"Additional 1GiB of RAM per each supported GPU",
		"Additional 1GiB of RAM per each supported SR-IOV NIC",
		"CPU has virtualization flag (vmx or svm)",
	}
	requirements := models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: o.GetDependencies(),
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Qualitative: qualitativeRequirements,
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: MasterCPU,
					RAMMib:   MasterMemory,
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Qualitative: qualitativeRequirements,
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: WorkerCPU,
					RAMMib:   WorkerMemory,
				},
			},
		},
	}
	return &requirements, nil
}

func (o *operator) getDevicesMemoryOverhead(host *models.Host) (int64, error) {
	if host.Inventory == "" {
		return 0, nil
	}
	inventory, err := hostutil.UnmarshalInventory(host.Inventory)
	if err != nil {
		return 0, err
	}
	gpuCount := o.getGPUCount(*inventory)
	srIovNicCount := o.getSrIovNicCount(*inventory)
	totalDevices := gpuCount + srIovNicCount

	// One device imposes 1GiB of additional memory requirement
	return conversions.GibToMib(totalDevices), nil
}

func (o *operator) getGPUCount(inventory models.Inventory) int64 {
	var gpuCount int64
	for _, gpu := range inventory.Gpus {
		if o.config.SupportedGPUs[getDeviceKeyForGPU(gpu)] {
			gpuCount++
		}
	}
	return gpuCount
}

func (o *operator) getSrIovNicCount(inventory models.Inventory) int64 {
	var srIovCount int64
	for _, nic := range inventory.Interfaces {
		if o.config.SupportedSRIOVNetworkIC[getDeviceKeyForInterface(nic)] {
			srIovCount++
		}
	}
	return srIovCount
}

func getDeviceKeyForGPU(gpu *models.Gpu) string {
	return getDeviceKey(gpu.VendorID, gpu.DeviceID)
}

func getDeviceKeyForInterface(nic *models.Interface) string {
	return getDeviceKey(sanitizeID(nic.Vendor), sanitizeID(nic.Product))
}

func sanitizeID(id string) string {
	return strings.TrimPrefix(strings.ToLower(id), "0x")
}

func getDeviceKey(vendorID string, deviceID string) string {
	return vendorID + ":" + deviceID
}
