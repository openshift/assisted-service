package hardware

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

//go:generate mockgen -source=validator.go -package=hardware -destination=mock_validator.go
type Validator interface {
	GetHostValidDisks(host *models.Host) ([]*models.Disk, error)
	GetHostRequirements() *models.VersionedHostRequirements
	GetHostInstallationPath(host *models.Host) string
	GetClusterHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirements, error)
	DiskIsEligible(ctx context.Context, disk *models.Disk, cluster *common.Cluster, host *models.Host) ([]string, error)
	ListEligibleDisks(inventory *models.Inventory) []*models.Disk
	GetInstallationDiskSpeedThresholdMs() int64
	// GetPreflightHardwareRequirements provides hardware (host) requirements that can be calculated only using cluster information.
	// Returned information describe requirements coming from OCP and OLM operators.
	// It should replace GetHostRequirements when its endpoint is not used anymore.
	GetPreflightHardwareRequirements(ctx context.Context, cluster *common.Cluster) (*models.PreflightHardwareRequirements, error)
}

func NewValidator(log logrus.FieldLogger, cfg ValidatorCfg, operatorsAPI operators.API) Validator {
	return &validator{
		ValidatorCfg: cfg,
		log:          log,
		operatorsAPI: operatorsAPI,
	}
}

type ValidatorCfg struct {
	MinCPUCores                      int64                        `envconfig:"HW_VALIDATOR_MIN_CPU_CORES" default:"2"`
	MinCPUCoresWorker                int64                        `envconfig:"HW_VALIDATOR_MIN_CPU_CORES_WORKER" default:"2"`
	MinCPUCoresMaster                int64                        `envconfig:"HW_VALIDATOR_MIN_CPU_CORES_MASTER" default:"4"`
	MinRamGib                        int64                        `envconfig:"HW_VALIDATOR_MIN_RAM_GIB" default:"8"`
	MinRamGibWorker                  int64                        `envconfig:"HW_VALIDATOR_MIN_RAM_GIB_WORKER" default:"8"`
	MinRamGibMaster                  int64                        `envconfig:"HW_VALIDATOR_MIN_RAM_GIB_MASTER" default:"16"`
	MinDiskSizeGb                    int64                        `envconfig:"HW_VALIDATOR_MIN_DISK_SIZE_GIB" default:"120"` // Env variable is GIB to not break infra
	MaximumAllowedTimeDiffMinutes    int64                        `envconfig:"HW_VALIDATOR_MAX_TIME_DIFF_MINUTES" default:"4"`
	InstallationDiskSpeedThresholdMs int64                        `envconfig:"HW_INSTALLATION_DISK_SPEED_THRESHOLD_MS" default:"10"`
	VersionedRequirements            VersionedRequirementsDecoder `envconfig:"HW_VALIDATOR_REQUIREMENTS" default:"[]"`
}

type validator struct {
	ValidatorCfg
	log          logrus.FieldLogger
	operatorsAPI operators.API
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
func (v *validator) DiskIsEligible(ctx context.Context, disk *models.Disk, cluster *common.Cluster, host *models.Host) ([]string, error) {
	var notEligibleReasons []string
	requirements, err := v.GetClusterHostRequirements(ctx, cluster, host)
	if err != nil {
		return nil, err
	}
	minSizeBytes := conversions.GbToBytes(requirements.Total.DiskSizeGb)
	if disk.SizeBytes < minSizeBytes {
		notEligibleReasons = append(notEligibleReasons,
			fmt.Sprintf(
				"Disk is too small (disk only has %s, but %s are required)",
				humanize.Bytes(uint64(disk.SizeBytes)), humanize.Bytes(uint64(minSizeBytes))))
	}

	if allowedDriveTypes := []string{"HDD", "SSD"}; !funk.ContainsString(allowedDriveTypes, disk.DriveType) {
		notEligibleReasons = append(notEligibleReasons,
			fmt.Sprintf("Drive type is %s, it must be one of %s.",
				disk.DriveType, strings.Join(allowedDriveTypes, ", ")))
	}

	return notEligibleReasons, nil
}

func (v *validator) ListEligibleDisks(inventory *models.Inventory) []*models.Disk {
	eligibleDisks := funk.Filter(inventory.Disks, func(disk *models.Disk) bool {
		return disk.InstallationEligibility.Eligible
	}).([]*models.Disk)

	// Sorting list by size increase
	sort.Slice(eligibleDisks, func(i, j int) bool {
		isNvme1 := isNvme(eligibleDisks[i].Name)
		isNvme2 := isNvme(eligibleDisks[j].Name)
		if isNvme1 != isNvme2 {
			return isNvme2
		}

		// HDD is before SSD
		switch v := strings.Compare(eligibleDisks[i].DriveType, eligibleDisks[j].DriveType); v {
		case 0:
			return eligibleDisks[i].SizeBytes < eligibleDisks[j].SizeBytes
		default:
			return v < 0
		}
	})

	return eligibleDisks
}

func (v *validator) GetHostRequirements() *models.VersionedHostRequirements {
	return &models.VersionedHostRequirements{
		MasterRequirements: &models.ClusterHostRequirementsDetails{
			CPUCores:                         v.ValidatorCfg.MinCPUCoresMaster,
			RAMMib:                           conversions.GibToMib(v.ValidatorCfg.MinRamGibMaster),
			DiskSizeGb:                       v.ValidatorCfg.MinDiskSizeGb,
			InstallationDiskSpeedThresholdMs: v.InstallationDiskSpeedThresholdMs,
		},
		WorkerRequirements: &models.ClusterHostRequirementsDetails{
			CPUCores:                         v.ValidatorCfg.MinCPUCoresWorker,
			RAMMib:                           conversions.GibToMib(v.ValidatorCfg.MinRamGibWorker),
			DiskSizeGb:                       v.ValidatorCfg.MinDiskSizeGb,
			InstallationDiskSpeedThresholdMs: v.InstallationDiskSpeedThresholdMs,
		},
	}
}

func (v *validator) GetClusterHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirements, error) {
	operatorsRequirements, err := v.operatorsAPI.GetRequirementsBreakdownForHostInCluster(ctx, cluster, host)
	if err != nil {
		return nil, err
	}

	ocpRequirements := v.getOCPHostRoleRequirementsForVersion(host.Role, cluster.OpenshiftVersion)
	total := totalizeRequirements(ocpRequirements, operatorsRequirements)
	return &models.ClusterHostRequirements{
		HostID:    *host.ID,
		Ocp:       &ocpRequirements,
		Operators: operatorsRequirements,
		Total:     &total,
	}, nil
}

func (v *validator) GetPreflightHardwareRequirements(ctx context.Context, cluster *common.Cluster) (*models.PreflightHardwareRequirements, error) {
	operatorsRequirements, err := v.operatorsAPI.GetPreflightRequirementsBreakdownForCluster(ctx, cluster)
	if err != nil {
		return nil, err
	}
	ocpRequirements, err := v.getPreflightOCPRequirements(cluster)
	if err != nil {
		return nil, err
	}

	return &models.PreflightHardwareRequirements{
		Operators: operatorsRequirements,
		Ocp:       ocpRequirements,
	}, nil
}

func (v *validator) GetInstallationDiskSpeedThresholdMs() int64 {
	return v.InstallationDiskSpeedThresholdMs
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
	}
	return total
}

func (v *validator) getOCPHostRoleRequirementsForVersion(role models.HostRole, openShiftVersion string) models.ClusterHostRequirementsDetails {
	requirements := v.getOCPRequirementsForVersion(openShiftVersion)
	if role == models.HostRoleMaster {
		return *requirements.MasterRequirements
	}
	return *requirements.WorkerRequirements
}

func (v *validator) getPreflightOCPRequirements(cluster *common.Cluster) (*models.HostTypeHardwareRequirementsWrapper, error) {
	requirements := v.getOCPRequirementsForVersion(cluster.OpenshiftVersion)
	return &models.HostTypeHardwareRequirementsWrapper{
		Master: &models.HostTypeHardwareRequirements{
			Quantitative: requirements.MasterRequirements,
		},
		Worker: &models.HostTypeHardwareRequirements{
			Quantitative: requirements.WorkerRequirements,
		},
	}, nil
}

func (v *validator) getOCPRequirementsForVersion(openShiftVersion string) *models.VersionedHostRequirements {
	requirements, err := v.VersionedRequirements.GetVersionedHostRequirements(openShiftVersion)
	if err != nil {
		requirements = v.GetHostRequirements()
	}
	return requirements
}
