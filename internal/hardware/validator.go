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
	GetHostRequirements(role models.HostRole) models.HostRequirementsRole
	GetHostInstallationPath(host *models.Host) string
	GetClusterHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirements, error)
	DiskIsEligible(disk *models.Disk) []string
	ListEligibleDisks(inventory *models.Inventory) []*models.Disk
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
func (v *validator) DiskIsEligible(disk *models.Disk) []string {
	var notEligibleReasons []string

	if minSizeBytes := conversions.GbToBytes(v.MinDiskSizeGb); disk.SizeBytes < minSizeBytes {
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

	return notEligibleReasons
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

func (v *validator) GetHostRequirements(role models.HostRole) models.HostRequirementsRole {
	if role == models.HostRoleMaster {
		return v.defaultMasterRequirements()
	}
	return v.defaultWorkerRequirements()
}

func (v *validator) GetHostRequirementsForVersion(role models.HostRole, openShiftVersion string) models.HostRequirementsRole {
	requirements, ok := v.VersionedRequirements[openShiftVersion]
	if role == models.HostRoleMaster {
		if ok && requirements.MasterRequirements != nil {
			return fromRequirements(requirements.MasterRequirements)
		}
		return v.defaultMasterRequirements()
	}

	if ok && requirements.WorkerRequirements != nil {
		return fromRequirements(requirements.WorkerRequirements)
	}
	return v.defaultWorkerRequirements()
}

func (v *validator) defaultWorkerRequirements() models.HostRequirementsRole {
	return models.HostRequirementsRole{
		CPUCores:   v.ValidatorCfg.MinCPUCoresWorker,
		RAMGib:     v.ValidatorCfg.MinRamGibWorker,
		DiskSizeGb: v.ValidatorCfg.MinDiskSizeGb,
	}
}

func (v *validator) defaultMasterRequirements() models.HostRequirementsRole {
	return models.HostRequirementsRole{
		CPUCores:   v.ValidatorCfg.MinCPUCoresMaster,
		RAMGib:     v.ValidatorCfg.MinRamGibMaster,
		DiskSizeGb: v.ValidatorCfg.MinDiskSizeGb,
	}
}

func fromRequirements(nodeRequirements *Requirements) models.HostRequirementsRole {
	return models.HostRequirementsRole{
		CPUCores:   nodeRequirements.CPUCores,
		RAMGib:     nodeRequirements.RAMGib,
		DiskSizeGb: nodeRequirements.DiskSizeGb,
	}
}

func (v *validator) GetClusterHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirements, error) {
	operatorsRequirements, err := v.operatorsAPI.GetRequirementsBreakdownForHostInCluster(ctx, cluster, host)
	if err != nil {
		return nil, err
	}

	hostRequirements := v.GetHostRequirementsForVersion(host.Role, cluster.OpenshiftVersion)
	ocpRequirements := models.ClusterHostRequirementsDetails{
		CPUCores:                         hostRequirements.CPUCores,
		RAMMib:                           conversions.GibToMib(hostRequirements.RAMGib),
		DiskSizeGb:                       hostRequirements.DiskSizeGb,
		InstallationDiskSpeedThresholdMs: hostRequirements.InstallationDiskSpeedThresholdMs,
	}
	total := totalizeRequirements(ocpRequirements, operatorsRequirements)
	return &models.ClusterHostRequirements{
		HostID:    *host.ID,
		Ocp:       &ocpRequirements,
		Operators: operatorsRequirements,
		Total:     &total,
	}, nil
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
