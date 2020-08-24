package hardware

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/thoas/go-funk"

	"github.com/sirupsen/logrus"

	"github.com/alecthomas/units"
	"github.com/openshift/assisted-service/models"
)

//go:generate mockgen -source=validator.go -package=hardware -destination=mock_validator.go
type Validator interface {
	GetHostValidDisks(host *models.Host) ([]*models.Disk, error)
	GetHostRequirements(role models.HostRole) models.HostRequirementsRole
}

func NewValidator(log logrus.FieldLogger, cfg ValidatorCfg) Validator {
	return &validator{
		ValidatorCfg: cfg,
		log:          log,
	}
}

type ValidatorCfg struct {
	MinCPUCores       int64 `envconfig:"HW_VALIDATOR_MIN_CPU_CORES" default:"2"`
	MinCPUCoresWorker int64 `envconfig:"HW_VALIDATOR_MIN_CPU_CORES_WORKER" default:"2"`
	MinCPUCoresMaster int64 `envconfig:"HW_VALIDATOR_MIN_CPU_CORES_MASTER" default:"4"`
	MinRamGib         int64 `envconfig:"HW_VALIDATOR_MIN_RAM_GIB" default:"8"`
	MinRamGibWorker   int64 `envconfig:"HW_VALIDATOR_MIN_RAM_GIB_WORKER" default:"8"`
	MinRamGibMaster   int64 `envconfig:"HW_VALIDATOR_MIN_RAM_GIB_MASTER" default:"16"`
	MinDiskSizeGb     int64 `envconfig:"HW_VALIDATOR_MIN_DISK_SIZE_GIB" default:"120"` // Env variable is GIB to not break infra
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
	disks := ListValidDisks(&inventory, gbToBytes(v.MinDiskSizeGb))
	if len(disks) == 0 {
		return nil, fmt.Errorf("host %s doesn't have valid disks", host.ID)
	}
	return disks, nil
}

func gbToBytes(gb int64) int64 {
	return gb * int64(units.GB)
}

func isNvme(name string) bool {
	return strings.HasPrefix(name, "nvme")
}

func ListValidDisks(inventory *models.Inventory, minSizeRequiredInBytes int64) []*models.Disk {
	var disks []*models.Disk
	for _, disk := range inventory.Disks {
		if disk.SizeBytes >= minSizeRequiredInBytes && funk.ContainsString([]string{"HDD", "SSD"}, disk.DriveType) {
			disks = append(disks, disk)
		}
	}

	// Sorting list by size increase
	sort.Slice(disks, func(i, j int) bool {
		isNvme1 := isNvme(disks[i].Name)
		isNvme2 := isNvme(disks[j].Name)
		if isNvme1 != isNvme2 {
			return isNvme2
		}

		// HDD is before SSD
		switch v := strings.Compare(disks[i].DriveType, disks[j].DriveType); v {
		case 0:
			return disks[i].SizeBytes < disks[j].SizeBytes
		default:
			return v < 0
		}
	})
	return disks
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
