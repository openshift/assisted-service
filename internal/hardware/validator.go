package hardware

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/sirupsen/logrus"

	"github.com/alecthomas/units"
	"github.com/openshift/assisted-service/models"
)

const diskNameFilterRegex = "nvme"

//go:generate mockgen -source=validator.go -package=hardware -destination=mock_validator.go
type Validator interface {
	GetHostValidDisks(host *models.Host) ([]*models.Disk, error)
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

func ListValidDisks(inventory *models.Inventory, minSizeRequiredInBytes int64) []*models.Disk {
	var disks []*models.Disk
	filter, _ := regexp.Compile(diskNameFilterRegex)
	for _, disk := range inventory.Disks {
		if disk.SizeBytes >= minSizeRequiredInBytes && disk.DriveType == "HDD" && !filter.MatchString(disk.Name) {
			disks = append(disks, disk)
		}
	}
	// Sorting list by size increase
	sort.Slice(disks, func(i, j int) bool {
		return disks[i].SizeBytes < disks[j].SizeBytes
	})
	return disks
}
