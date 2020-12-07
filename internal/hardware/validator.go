package hardware

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dustin/go-humanize"

	"github.com/thoas/go-funk"

	"github.com/sirupsen/logrus"

	"github.com/alecthomas/units"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

//go:generate mockgen -source=validator.go -package=hardware -destination=mock_validator.go
type Validator interface {
	GetHostValidDisks(host *models.Host) ([]*models.Disk, error)
	GetHostRequirements(role models.HostRole) models.HostRequirementsRole
	DiskIsEligible(disk *models.Disk) (relevant bool, notEligibleReasons []string)
}

func NewValidator(log logrus.FieldLogger, cfg ValidatorCfg) Validator {
	return &validator{
		ValidatorCfg: cfg,
		log:          log,
	}
}

type ValidatorCfg struct {
	MinCPUCores                   int64 `envconfig:"HW_VALIDATOR_MIN_CPU_CORES" default:"2"`
	MinCPUCoresWorker             int64 `envconfig:"HW_VALIDATOR_MIN_CPU_CORES_WORKER" default:"2"`
	MinCPUCoresMaster             int64 `envconfig:"HW_VALIDATOR_MIN_CPU_CORES_MASTER" default:"4"`
	MinRamGib                     int64 `envconfig:"HW_VALIDATOR_MIN_RAM_GIB" default:"8"`
	MinRamGibWorker               int64 `envconfig:"HW_VALIDATOR_MIN_RAM_GIB_WORKER" default:"8"`
	MinRamGibMaster               int64 `envconfig:"HW_VALIDATOR_MIN_RAM_GIB_MASTER" default:"16"`
	MinDiskSizeGb                 int64 `envconfig:"HW_VALIDATOR_MIN_DISK_SIZE_GIB" default:"120"` // Env variable is GIB to not break infra
	MaximumAllowedTimeDiffMinutes int64 `envconfig:"HW_VALIDATOR_MAX_TIME_DIFF_MINUTES" default:"4"`
}

type validator struct {
	ValidatorCfg
	log logrus.FieldLogger
}

func (v *validator) GetHostValidDisks(host *models.Host) ([]*models.Disk, error) {
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return nil, err
	}
	disks := ListValidDisks(&inventory)
	if len(disks) == 0 {
		return nil, errors.Errorf("host %s doesn't have valid disks", host.ID)
	}
	return disks, nil
}

func gbToBytes(gb int64) int64 {
	return gb * int64(units.GB)
}

func isNvme(name string) bool {
	return strings.HasPrefix(name, "nvme")
}

// DiskIsEligible checks if a disk is relevant for installation by testing
// it against a list of predicates. Also returns all the reasons the disk
// was found to be not eligible
func (v *validator) DiskIsEligible(disk *models.Disk) (relevant bool, notEligibleReasons []string) {
	if minSizeBytes := gbToBytes(v.MinDiskSizeGb); disk.SizeBytes < minSizeBytes {
		notEligibleReasons = append(notEligibleReasons,
			fmt.Sprintf("Disk is too small (disk only has %s, but %s are required)", humanize.Bytes(uint64(disk.SizeBytes)), humanize.Bytes(uint64(minSizeBytes))))
	}

	if allowedDriveTypes := []string{"HDD", "SSD"}; !funk.ContainsString(allowedDriveTypes, disk.DriveType) {
		notEligibleReasons = append(notEligibleReasons,
			fmt.Sprintf("Drive type is %s, it must be one of %s.",
				disk.DriveType, strings.Join(allowedDriveTypes, ", ")))
	}

	relevant = len(notEligibleReasons) == 0

	return
}

func ListValidDisks(inventory *models.Inventory) []*models.Disk {
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
		return models.HostRequirementsRole{
			CPUCores:   v.ValidatorCfg.MinCPUCoresMaster,
			RAMGib:     v.ValidatorCfg.MinRamGibMaster,
			DiskSizeGb: v.ValidatorCfg.MinDiskSizeGb,
		}
	} else {
		return models.HostRequirementsRole{
			CPUCores:   v.ValidatorCfg.MinCPUCoresWorker,
			RAMGib:     v.ValidatorCfg.MinRamGibWorker,
			DiskSizeGb: v.ValidatorCfg.MinDiskSizeGb,
		}
	}
}
