package hardware

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/feature"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"k8s.io/utils/ptr"
)

const (
	tooSmallDiskTemplate       = "Disk is too small (disk only has %s, but %s are required)"
	wrongDriveTypeTemplate     = "Drive type is %s, it must be one of %s."
	wrongMultipathTypeTemplate = "Multipath device has path of type %s, it must be %s"
	iSCSIWithMultipathHolder   = "iSCSI disk with a multipath holder is not eligible"
	wrongISCSINetworkTemplate  = "iSCSI host IP %s is the same as host IP, they must be different"
)

//go:generate mockgen -source=validator.go -package=hardware -destination=mock_validator.go
type Validator interface {
	GetHostValidDisks(host *models.Host) ([]*models.Disk, error)
	GetHostInstallationPath(host *models.Host) string
	GetClusterHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirements, error)
	GetInfraEnvHostRequirements(ctx context.Context, infraEnv *common.InfraEnv) (*models.ClusterHostRequirements, error)
	DiskIsEligible(ctx context.Context, disk *models.Disk, infraEnv *common.InfraEnv, cluster *common.Cluster, host *models.Host, inventory *models.Inventory) ([]string, error)
	ListEligibleDisks(inventory *models.Inventory) []*models.Disk
	IsValidStorageDeviceType(disk *models.Disk, hostArchitecture string, openshiftVersion string) bool
	GetInstallationDiskSpeedThresholdMs(ctx context.Context, cluster *common.Cluster, host *models.Host) (int64, error)
	// GetPreflightHardwareRequirements provides hardware (host) requirements that can be calculated only using cluster information.
	// Returned information describe requirements coming from OCP and OLM operators.
	GetPreflightHardwareRequirements(ctx context.Context, cluster *common.Cluster) (*models.PreflightHardwareRequirements, error)
	GetPreflightInfraEnvHardwareRequirements(ctx context.Context, infraEnv *common.InfraEnv) (*models.PreflightHardwareRequirements, error)
}

func NewValidator(log logrus.FieldLogger, cfg ValidatorCfg, operatorsAPI operators.API, reg registry.ProviderRegistry) Validator {
	diskEligibilityMatchers := []*regexp.Regexp{
		compileDiskReasonTemplate(tooSmallDiskTemplate, ".*", ".*"),
		compileDiskReasonTemplate(wrongDriveTypeTemplate, ".*", ".*"),
		compileDiskReasonTemplate(wrongMultipathTypeTemplate, ".*", ".*"),
	}

	return &validator{
		ValidatorCfg:            cfg,
		log:                     log,
		operatorsAPI:            operatorsAPI,
		diskEligibilityMatchers: diskEligibilityMatchers,
		edgeWorkersProductList:  strings.Split(strings.ToLower(strings.ReplaceAll(cfg.EdgeWorkerProductNames, " ", "")), ","),
		providerRegistry:        reg,
	}
}

type ValidatorCfg struct {
	feature.Flags

	MaximumAllowedTimeDiffMinutes int64                        `envconfig:"HW_VALIDATOR_MAX_TIME_DIFF_MINUTES" default:"4"`
	VersionedRequirements         VersionedRequirementsDecoder `envconfig:"HW_VALIDATOR_REQUIREMENTS" default:"[]"`
	MaxHostDisconnectionTime      time.Duration                `envconfig:"HOST_MAX_DISCONNECTION_TIME" default:"3m"`
	AgentDockerImage              string                       `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer-agent:latest"`
	EdgeWorkerProductNames        string                       `envconfig:"EDGE_WORKERS_PRODUCT_NAMES" default:"BlueField SoC"`
}

type validator struct {
	ValidatorCfg
	log                     logrus.FieldLogger
	operatorsAPI            operators.API
	diskEligibilityMatchers []*regexp.Regexp
	edgeWorkersProductList  []string
	providerRegistry        registry.ProviderRegistry
}

// DiskEligibilityInitialized is used to detect inventories created by older versions of the agent/service
func DiskEligibilityInitialized(disk *models.Disk) bool {
	return disk.InstallationEligibility.Eligible || len(disk.InstallationEligibility.NotEligibleReasons) != 0
}

func (v *validator) GetHostInstallationPath(host *models.Host) string {
	return hostutil.GetHostInstallationPath(host)
}

func (v *validator) GetHostValidDisks(host *models.Host) ([]*models.Disk, error) {
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return nil, err
	}
	return v.ListEligibleDisks(&inventory), nil
}

func isNvme(name string) bool {
	return strings.HasPrefix(name, "nvme")
}

// DiskIsEligible checks if a disk is eligible for installation by testing
// it against a list of predicates. Returns all the reasons the disk
// was found to be not eligible, or an empty slice if it was found to
// be eligible
func (v *validator) DiskIsEligible(ctx context.Context, disk *models.Disk, infraEnv *common.InfraEnv, cluster *common.Cluster, host *models.Host, inventory *models.Inventory) ([]string, error) {
	var requirements *models.ClusterHostRequirements
	var err error
	var clusterVersion string
	if cluster != nil {
		requirements, err = v.GetClusterHostRequirements(ctx, cluster, host)
		clusterVersion = cluster.OpenshiftVersion
	} else {
		requirements, err = v.GetInfraEnvHostRequirements(ctx, infraEnv)
		clusterVersion = ""
	}
	if err != nil {
		return nil, err
	}
	// This method can be called on demand, so the disk may already have service non-eligibility reasons
	notEligibleReasons := v.purgeServiceReasons(disk.InstallationEligibility.NotEligibleReasons)

	minSizeBytes := conversions.GbToBytes(requirements.Total.DiskSizeGb)
	if disk.SizeBytes < minSizeBytes {
		notEligibleReasons = append(notEligibleReasons,
			fmt.Sprintf(
				tooSmallDiskTemplate,
				humanize.Bytes(uint64(disk.SizeBytes)), humanize.Bytes(uint64(minSizeBytes))))
	}

	hostArchitecture := inventory.CPU.Architecture
	if !v.IsValidStorageDeviceType(disk, hostArchitecture, clusterVersion) {
		notEligibleReasons = append(notEligibleReasons,
			fmt.Sprintf(wrongDriveTypeTemplate, disk.DriveType, strings.Join(v.getValidDeviceStorageTypes(hostArchitecture, clusterVersion), ", ")))
	}

	// We only allow multipath if all paths are FC
	if disk.DriveType == models.DriveTypeMultipath {
		for _, inventoryDisk := range inventory.Disks {
			if funk.ContainsString(strings.Split(inventoryDisk.Holders, ","), disk.Name) {
				if inventoryDisk.DriveType != models.DriveTypeFC {
					notEligibleReasons = append(notEligibleReasons,
						fmt.Sprintf(wrongMultipathTypeTemplate, inventoryDisk.DriveType, string(models.DriveTypeFC)))
					break
				}
			}
		}
	}

	if disk.DriveType == models.DriveTypeISCSI {
		err := areISCSIHoldersValid(disk, inventory)
		if err != nil {
			notEligibleReasons = append(notEligibleReasons, err.Error())
		}

		// Check if network is configured properly to install on iSCSI boot drive
		err = isISCSINetworkingValid(disk, inventory)
		if err != nil {
			notEligibleReasons = append(notEligibleReasons, err.Error())
		}
	}

	return notEligibleReasons, nil
}

// Validate holders of iSCSI disk. We do not allow iSCSI disk with multipath holder.
func areISCSIHoldersValid(disk *models.Disk, inventory *models.Inventory) error {
	multipathDiskNamesMap := make(map[string]struct{})
	for _, inventoryDisk := range inventory.Disks {
		if inventoryDisk.DriveType == models.DriveTypeMultipath {
			multipathDiskNamesMap[inventoryDisk.Name] = struct{}{}
		}
	}

	// Check if the iSCSI disk has any holders that are multipath disks
	holders := strings.Split(disk.Holders, ",")
	for _, holder := range holders {
		if _, exists := multipathDiskNamesMap[holder]; exists {
			return fmt.Errorf(iSCSIWithMultipathHolder)
		}
	}

	return nil

}

// isISCSINetworkingValid checks if the iSCSI dish is not connected through the
// default network interface. The default network interface is the interface
// which is used by the default gateway.
func isISCSINetworkingValid(disk *models.Disk, inventory *models.Inventory) error {
	// get the IPv4 or the IPv6 of the interface connected to the iSCSI target
	if disk.Iscsi == nil {
		return fmt.Errorf("Host IP address is not available")

	}
	iSCSIHostIP, err := netip.ParseAddr(disk.Iscsi.HostIPAddress)
	if err != nil {
		return fmt.Errorf("Cannot parse iSCSI host IP %s: %w", disk.Iscsi.HostIPAddress, err)
	}

	defaultRoute := network.GetDefaultRouteByFamily(inventory.Routes, iSCSIHostIP.Is6())
	if defaultRoute == nil {
		return fmt.Errorf("Cannot find default route")
	}

	defaultInterface := funk.Find(inventory.Interfaces, func(i *models.Interface) bool {
		return i.Name == defaultRoute.Interface
	}).(*models.Interface)
	if defaultInterface == nil {
		return fmt.Errorf("Cannot find the network interface behind the default route")
	}

	// look if one of the IP assigned to the default interface interface
	// corresponds to the IP used by host to connect on the iSCSI target
	ips := defaultInterface.IPV4Addresses
	if iSCSIHostIP.Is6() {
		ips = defaultInterface.IPV6Addresses
	}
	found := funk.Find(ips, func(ip string) bool {
		prefix, err := netip.ParsePrefix(ip)
		return err == nil && iSCSIHostIP.Compare(prefix.Addr()) == 0
	})

	if found != nil {
		return fmt.Errorf(wrongISCSINetworkTemplate, iSCSIHostIP.String())
	}
	return nil
}

func (v *validator) IsValidStorageDeviceType(disk *models.Disk, hostArchitecture string, openshiftVersion string) bool {
	return funk.ContainsString(v.getValidDeviceStorageTypes(hostArchitecture, openshiftVersion), string(disk.DriveType))
}

func (v *validator) purgeServiceReasons(reasons []string) []string {
	var notEligibleReasons []string
	for _, reason := range reasons {
		var matches bool
		for _, matcher := range v.diskEligibilityMatchers {
			if matcher.MatchString(reason) {
				matches = true
				break
			}
		}
		if !matches {
			notEligibleReasons = append(notEligibleReasons, reason)
		}
	}
	return notEligibleReasons
}

func (v *validator) ListEligibleDisks(inventory *models.Inventory) []*models.Disk {
	eligibleDisks := funk.Filter(inventory.Disks, func(disk *models.Disk) bool {
		return disk.InstallationEligibility.Eligible
	}).([]*models.Disk)

	// Sorting list by size increase
	sort.Slice(eligibleDisks, func(i, j int) bool {
		// 1. First, choose non-NVMe drives before NVMe drives (leave
		// the faster drives for workload storage)
		isNvme1 := isNvme(eligibleDisks[i].Name)
		isNvme2 := isNvme(eligibleDisks[j].Name)
		// If i is NVMe and j isn't, return false (j first)
		// If j is NVMe and i isn't, return true (i first)
		if isNvme1 != isNvme2 {
			return isNvme2
		}

		// 2. Sort by DriveType - HDD before SSD (leave the faster
		// drives for workload storage)
		if v := strings.Compare(string(eligibleDisks[i].DriveType), string(eligibleDisks[j].DriveType)); v != 0 {
			return v < 0
		}

		// 3. Sort according to disk size (use the smallest valid
		// disk size to leave more capacity for workload storage)
		if eligibleDisks[i].SizeBytes != eligibleDisks[j].SizeBytes {
			return eligibleDisks[i].SizeBytes < eligibleDisks[j].SizeBytes
		}

		// 4. Sort according to HCTL which indicates physical order
		// (increases the chance of booting from the correct disk
		// after reboot)
		return eligibleDisks[i].Hctl < eligibleDisks[j].Hctl
	})

	return eligibleDisks
}

func (v *validator) GetClusterHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirements, error) {
	operatorsRequirements, err := v.operatorsAPI.GetRequirementsBreakdownForHostInCluster(ctx, cluster, host)
	if err != nil {
		return nil, err
	}

	ocpRequirements, err := v.getOCPClusterHostRoleRequirementsForVersion(cluster, host)
	if err != nil {
		return nil, err
	}
	total := totalizeRequirements(ocpRequirements, operatorsRequirements)
	return &models.ClusterHostRequirements{
		HostID:    *host.ID,
		Ocp:       &ocpRequirements,
		Operators: operatorsRequirements,
		Total:     &total,
	}, nil
}

func (v *validator) GetInfraEnvHostRequirements(ctx context.Context, infraEnv *common.InfraEnv) (*models.ClusterHostRequirements, error) {
	masterOcpRequirements, err := v.getOCPInfraEnvHostRoleRequirementsForVersion(infraEnv, models.HostRoleMaster)
	if err != nil {
		return nil, err
	}
	workerOcpRequirements, err := v.getOCPInfraEnvHostRoleRequirementsForVersion(infraEnv, models.HostRoleWorker)
	if err != nil {
		return nil, err
	}

	requirements := &workerOcpRequirements
	if workerOcpRequirements.DiskSizeGb > masterOcpRequirements.DiskSizeGb {
		requirements = &masterOcpRequirements
	}

	return &models.ClusterHostRequirements{
		HostID:    "",
		Ocp:       requirements,
		Operators: nil,
		Total:     requirements,
	}, nil
}

func isDiskEncryptionSetWithTpm(c *common.Cluster) bool {
	return c.DiskEncryption != nil &&
		swag.StringValue(c.DiskEncryption.EnableOn) != models.DiskEncryptionEnableOnNone &&
		swag.StringValue(c.DiskEncryption.Mode) == models.DiskEncryptionModeTpmv2
}

func (v *validator) GetPreflightHardwareRequirements(ctx context.Context, cluster *common.Cluster) (*models.PreflightHardwareRequirements, error) {
	operatorsRequirements, err := v.operatorsAPI.GetPreflightRequirementsBreakdownForCluster(ctx, cluster)
	if err != nil {
		return nil, err
	}
	ocpRequirements, err := v.getClusterPreflightOCPRequirements(cluster)
	if err != nil {
		return nil, err
	}
	if isDiskEncryptionSetWithTpm(cluster) {
		switch swag.StringValue(cluster.DiskEncryption.EnableOn) {
		case models.DiskEncryptionEnableOnAll:
			ocpRequirements.Master.Quantitative.TpmEnabledInBios = true
			ocpRequirements.Worker.Quantitative.TpmEnabledInBios = true
		case models.DiskEncryptionEnableOnMasters:
			ocpRequirements.Master.Quantitative.TpmEnabledInBios = true
		case models.DiskEncryptionEnableOnWorkers:
			ocpRequirements.Worker.Quantitative.TpmEnabledInBios = true
		default:
			return nil, fmt.Errorf("disk-encryption is enabled on non-valid role: %s", swag.StringValue(cluster.DiskEncryption.EnableOn))
		}
	}

	return &models.PreflightHardwareRequirements{
		Operators: operatorsRequirements,
		Ocp:       ocpRequirements,
	}, nil
}

func (v *validator) GetPreflightInfraEnvHardwareRequirements(ctx context.Context, infraEnv *common.InfraEnv) (*models.PreflightHardwareRequirements, error) {
	ocpRequirements, err := v.getInfraEnvPreflightOCPRequirements(infraEnv)
	if err != nil {
		return nil, err
	}

	return &models.PreflightHardwareRequirements{
		Operators: nil,
		Ocp:       ocpRequirements,
	}, nil
}

func (v *validator) GetInstallationDiskSpeedThresholdMs(ctx context.Context, cluster *common.Cluster, host *models.Host) (int64, error) {
	ocpRequirements, err := v.getOCPClusterHostRoleRequirementsForVersion(cluster, host)
	if err != nil {
		return 0, err
	}
	return ocpRequirements.InstallationDiskSpeedThresholdMs, nil
}

func totalizeRequirements(ocpRequirements models.ClusterHostRequirementsDetails, operatorRequirements []*models.OperatorHostRequirements) models.ClusterHostRequirementsDetails {
	total := ocpRequirements

	for _, req := range operatorRequirements {
		details := req.Requirements
		total.RAMMib = total.RAMMib + details.RAMMib
		total.CPUCores = total.CPUCores + details.CPUCores
		total.DiskSizeGb = total.DiskSizeGb + details.DiskSizeGb

		if details.InstallationDiskSpeedThresholdMs > 0 {
			if total.InstallationDiskSpeedThresholdMs == 0 || details.InstallationDiskSpeedThresholdMs < total.InstallationDiskSpeedThresholdMs {
				total.InstallationDiskSpeedThresholdMs = details.InstallationDiskSpeedThresholdMs
			}
		}
		if details.NetworkLatencyThresholdMs != nil && *details.NetworkLatencyThresholdMs >= 0 {
			if total.NetworkLatencyThresholdMs == nil {
				total.NetworkLatencyThresholdMs = details.NetworkLatencyThresholdMs
			} else {
				total.NetworkLatencyThresholdMs = ptr.To(math.Min(*total.NetworkLatencyThresholdMs, *details.NetworkLatencyThresholdMs))
			}
		}
		if details.PacketLossPercentage != nil && *details.PacketLossPercentage >= 0 {
			if total.PacketLossPercentage == nil {
				total.PacketLossPercentage = details.PacketLossPercentage
			} else {
				total.PacketLossPercentage = ptr.To(math.Min(*total.PacketLossPercentage, *details.PacketLossPercentage))
			}
		}
	}
	return total
}

func (v *validator) getOCPClusterHostRoleRequirementsForVersion(cluster *common.Cluster, host *models.Host) (models.ClusterHostRequirementsDetails, error) {
	requirements, err := v.getOCPRequirementsForVersion(cluster.OpenshiftVersion)
	if err != nil {
		return models.ClusterHostRequirementsDetails{}, err
	}

	if common.GetEffectiveRole(host) == models.HostRoleMaster {
		if common.IsSingleNodeCluster(cluster) {
			return *requirements.SNORequirements, nil
		}
		return *requirements.MasterRequirements, nil
	}

	if v.isEdgeWorker(host) {
		return *requirements.EdgeWorkerRequirements, nil
	}

	return *requirements.WorkerRequirements, nil
}

// There is no need to fail here, failed to get inventory just return false
func (v *validator) isEdgeWorker(host *models.Host) bool {
	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		return false
	}
	if inventory.CPU.Architecture != common.AARCH64CPUArchitecture {
		return false
	}

	return funk.Contains(v.edgeWorkersProductList, strings.ToLower(strings.ReplaceAll(inventory.SystemVendor.ProductName, " ", "")))
}

func (v *validator) getOCPInfraEnvHostRoleRequirementsForVersion(infraEnv *common.InfraEnv, role models.HostRole) (models.ClusterHostRequirementsDetails, error) {
	requirements, err := v.getOCPRequirementsForVersion(infraEnv.OpenshiftVersion)
	if err != nil {
		return models.ClusterHostRequirementsDetails{}, err
	}

	if role == models.HostRoleMaster {
		return *requirements.MasterRequirements, nil
	}
	if role == models.HostRoleWorker || role == models.HostRoleAutoAssign {
		return *requirements.WorkerRequirements, nil
	}
	return models.ClusterHostRequirementsDetails{}, fmt.Errorf("Invalid role for host %s", role)
}

func (v *validator) getClusterPreflightOCPRequirements(cluster *common.Cluster) (*models.HostTypeHardwareRequirementsWrapper, error) {
	requirements, err := v.getOCPRequirementsForVersion(cluster.OpenshiftVersion)
	if err != nil {
		return nil, err
	}
	return &models.HostTypeHardwareRequirementsWrapper{
		Master: &models.HostTypeHardwareRequirements{
			Quantitative: v.getMasterRequirements(cluster, requirements),
		},
		Worker: &models.HostTypeHardwareRequirements{
			Quantitative: requirements.WorkerRequirements,
		},
	}, nil
}

func (v *validator) getInfraEnvPreflightOCPRequirements(infraEnv *common.InfraEnv) (*models.HostTypeHardwareRequirementsWrapper, error) {
	requirements, err := v.getOCPRequirementsForVersion(infraEnv.OpenshiftVersion)
	if err != nil {
		return nil, err
	}
	return &models.HostTypeHardwareRequirementsWrapper{
		Master: &models.HostTypeHardwareRequirements{
			Quantitative: requirements.MasterRequirements,
		},
		Worker: &models.HostTypeHardwareRequirements{
			Quantitative: requirements.WorkerRequirements,
		},
	}, nil
}

func (v *validator) getMasterRequirements(cluster *common.Cluster, requirements *models.VersionedHostRequirements) *models.ClusterHostRequirementsDetails {
	if common.IsSingleNodeCluster(cluster) {
		return requirements.SNORequirements
	}
	return requirements.MasterRequirements
}

func (v *validator) getOCPRequirementsForVersion(openshiftVersion string) (*models.VersionedHostRequirements, error) {
	return v.VersionedRequirements.GetVersionedHostRequirements(openshiftVersion)
}

func (v *validator) getValidDeviceStorageTypes(hostArchitecture string, openshiftVersion string) []string {
	validTypes := []string{string(models.DriveTypeHDD), string(models.DriveTypeSSD), string(models.DriveTypeMultipath)}

	isGreater, err := common.BaseVersionGreaterOrEqual("4.15.0", openshiftVersion)
	if err == nil && isGreater {
		validTypes = append(validTypes, string(models.DriveTypeISCSI))
	}

	if hostArchitecture == models.ClusterCPUArchitectureS390x {
		validTypes = append(validTypes, string(models.DriveTypeFC), string(models.DriveTypeECKDESE), string(models.DriveTypeECKD), string(models.DriveTypeFBA))
	}

	return validTypes
}

func compileDiskReasonTemplate(template string, wildcards ...interface{}) *regexp.Regexp {
	tmp, err := regexp.Compile(fmt.Sprintf(regexp.QuoteMeta(template), wildcards...))
	if err != nil {
		panic(err)
	}
	return tmp
}
