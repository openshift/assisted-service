package cnv

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/internal/hardware/virt"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/lvm"
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

func (o *operator) GetFullName() string {
	return "OpenShift Virtualization"
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	lsoOperator := []string{lso.Operator.Name}
	lvmOperator := []string{lvm.Operator.Name}

	if cluster.OpenshiftVersion == "" {
		return lsoOperator, nil
	}

	if isGreaterOrEqual, _ := common.BaseVersionGreaterOrEqual(lvm.LvmMinMultiNodeSupportVersion, cluster.OpenshiftVersion); isGreaterOrEqual {
		return lvmOperator, nil
	}

	// SNO
	if common.IsSingleNodeCluster(cluster) {
		if isGreaterOrEqual, _ := common.BaseVersionGreaterOrEqual(lvm.LvmsMinOpenshiftVersion4_12, cluster.OpenshiftVersion); isGreaterOrEqual {
			return lvmOperator, nil
		}
	}

	return lsoOperator, nil
}

// GetClusterValidationID returns cluster validation ID for the Operator
func (o *operator) GetClusterValidationID() string {
	return string(models.ClusterValidationIDCnvRequirementsSatisfied)
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return string(models.HostValidationIDCnvRequirementsSatisfied)
}

// ValidateCluster verifies whether this operator is valid for given cluster
func (o *operator) ValidateCluster(_ context.Context, cluster *common.Cluster) (api.ValidationResult, error) {
	status, message := o.validateRequirements(&cluster.Cluster)
	return api.ValidationResult{Status: status, ValidationId: o.GetClusterValidationID(), Reasons: []string{message}}, nil
}

func (o *operator) validateRequirements(cluster *models.Cluster) (api.ValidationStatus, string) {
	if !featuresupport.IsFeatureCompatibleWithArchitecture(models.FeatureSupportLevelIDCNV, cluster.OpenshiftVersion, cluster.CPUArchitecture) {
		return api.Failure, fmt.Sprintf(
			"%s is supported only for %s CPU architecture.", o.GetFullName(), common.DefaultCPUArchitecture)
	}
	return api.Success, ""
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

	if !virt.IsVirtSupported(inventory) {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{"CPU does not have virtualization support"}}, nil
	}

	if shouldInstallHPP(o.config, cluster) {
		if err = validDiscoverableSNODisk(inventory.Disks, host.InstallationDiskID, o.config.SNOPoolSizeRequestHPPGib); err != nil {
			return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{err.Error()}}, nil
		}
	}

	role := common.GetEffectiveRole(host)

	// If the Role is set to Auto-assign for a host, it is not possible to determine whether the node will end up as a master or worker node.
	if role == models.HostRoleAutoAssign {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{"All host roles must be assigned to enable OpenShift Virtualization"}}, nil
	}
	requirements, err := o.GetHostRequirements(ctx, cluster, host)
	if err != nil {
		message := fmt.Sprintf("Failed to get host requirements for host with id %s", host.ID)
		o.log.Error(message)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{message, err.Error()}}, err
	}

	cpu := requirements.CPUCores
	if inventory.CPU.Count < cpu {
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{fmt.Sprintf("Insufficient CPU to deploy OpenShift Virtualization. Required CPU count is %d but found %d ", cpu, inventory.CPU.Count)}}, nil
	}

	mem := requirements.RAMMib
	memBytes := conversions.MibToBytes(mem)
	if inventory.Memory.UsableBytes < memBytes {
		usableMemory := conversions.BytesToMib(inventory.Memory.UsableBytes)
		return api.ValidationResult{Status: api.Failure, ValidationId: o.GetHostValidationID(), Reasons: []string{fmt.Sprintf("Insufficient memory to deploy OpenShift Virtualization. Required memory is %d MiB but found %d MiB", mem, usableMemory)}}, nil
	}

	// TODO: validate available devices on worker node like gpu and sr-iov and check whether there is enough memory to support them
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID()}, nil
}

// GenerateManifests generates manifests for the operator
func (o *operator) GenerateManifests(c *common.Cluster) (map[string][]byte, []byte, error) {
	return Manifests(o.config, c)
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
		log.WithError(err).Errorf("Cannot Retrieve preflight requirements for cluster %s", cluster.ID)
		return nil, err
	}

	if common.IsSingleNodeCluster(cluster) {
		overhead, err := o.getDevicesMemoryOverhead(host)
		if err != nil {
			log.WithError(err).WithField("inventory", host.Inventory).Errorf("Cannot parse inventory for host %v", host.ID)
			return nil, err
		}
		preflightRequirements.Requirements.Master.Quantitative.RAMMib += overhead
		return preflightRequirements.Requirements.Master.Quantitative, nil
	}

	role := common.GetEffectiveRole(host)
	switch role {
	case models.HostRoleMaster:
		return preflightRequirements.Requirements.Master.Quantitative, nil
	case models.HostRoleWorker, models.HostRoleAutoAssign:
		return o.getWorkerRequirements(ctx, cluster, host, preflightRequirements)
	}
	return nil, fmt.Errorf("unsupported role: %s", role)
}

func (o *operator) getWorkerRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host, preflightRequirements *models.OperatorHardwareRequirements) (*models.ClusterHostRequirementsDetails, error) {
	log := logutil.FromContext(ctx, o.log)
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

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(_ context.Context, cluster *common.Cluster) (*models.OperatorHardwareRequirements, error) {
	qualitativeRequirements := []string{
		"Additional 1GiB of RAM per each supported GPU",
		"Additional 1GiB of RAM per each supported SR-IOV NIC",
		"CPU has virtualization flag (vmx or svm)",
	}
	if shouldInstallHPP(o.config, cluster) {
		qualitativeRequirements = append(qualitativeRequirements, fmt.Sprintf("Additional disk with %d Gi", o.config.SNOPoolSizeRequestHPPGib))
	}

	cnvODependencies, err := o.GetDependencies(cluster)
	if err != nil {
		return &models.OperatorHardwareRequirements{}, err
	}
	requirements := models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: cnvODependencies,
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

	if common.IsSingleNodeCluster(cluster) {
		requirements.Requirements.Master.Quantitative.CPUCores += WorkerCPU
		requirements.Requirements.Master.Quantitative.RAMMib += WorkerMemory
	}

	return &requirements, nil
}

func (o *operator) getDevicesMemoryOverhead(host *models.Host) (int64, error) {
	if host.Inventory == "" {
		return 0, nil
	}
	inventory, err := common.UnmarshalInventory(host.Inventory)
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

// If CNV is deployed on SNO, we want at least one non bootable disk (i.e. discoverable by LSO)
// with certain size threshold for the hpp storage pool
func validDiscoverableSNODisk(disks []*models.Disk, installationDiskID string, diskThresholdGi int64) error {
	thresholdBytes := conversions.GibToBytes(diskThresholdGi)
	thresholdGB := conversions.BytesToGb(thresholdBytes)

	for _, disk := range disks {
		if (disk.DriveType == models.DriveTypeSSD || disk.DriveType == models.DriveTypeHDD) && installationDiskID != disk.ID && disk.SizeBytes != 0 {
			if disk.SizeBytes > thresholdBytes {
				return nil
			}
		}
	}
	return fmt.Errorf("OpenShift Virtualization on SNO requires an additional disk with %d GB (%d Gi) in order to provide persistent storage for VMs, using hostpath-provisioner", thresholdGB, diskThresholdGi)
}

func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDCNV
}
