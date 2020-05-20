package hardware

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/alecthomas/units"
	"github.com/filanov/bm-inventory/models"
)

type IsSufficientReply struct {
	IsSufficient bool
	Reason       string
}

const diskNameFilterRegex = "nvme"

//go:generate mockgen -source=validator.go -package=hardware -destination=mock_validator.go
type Validator interface {
	IsSufficient(host *models.Host) (*IsSufficientReply, error)
	GetHostValidDisks(host *models.Host) ([]*models.Disk, error)
}

func NewValidator(cfg ValidatorCfg) Validator {
	return &validator{ValidatorCfg: cfg}
}

type ValidatorCfg struct {
	MinCPUCores       int64 `envconfig:"HW_VALIDATOR_MIN_CPU_CORES" default:"2"`
	MinCPUCoresWorker int64 `envconfig:"HW_VALIDATOR_MIN_CPU_CORES_WORKER" default:"2"`
	MinCPUCoresMaster int64 `envconfig:"HW_VALIDATOR_MIN_CPU_CORES_MASTER" default:"4"`
	MinRamGib         int64 `envconfig:"HW_VALIDATOR_MIN_RAM_GIB" default:"8"`
	MinRamGibWorker   int64 `envconfig:"HW_VALIDATOR_MIN_RAM_GIB_WORKER" default:"8"`
	MinRamGibMaster   int64 `envconfig:"HW_VALIDATOR_MIN_RAM_GIB_MASTER" default:"16"`
	MinDiskSizeGib    int64 `envconfig:"HW_VALIDATOR_MIN_DISK_SIZE_GIB" default:"120"`
}

type validator struct {
	ValidatorCfg
}

func (v *validator) IsSufficient(host *models.Host) (*IsSufficientReply, error) {
	var err error
	var reason string
	var isSufficient bool
	var hwInfo models.Inventory

	if err = json.Unmarshal([]byte(host.Inventory), &hwInfo); err != nil {
		return nil, err
	}

	var minCpuCoresRequired int64 = v.MinCPUCores
	var minRamRequired int64 = gibToBytes(v.MinRamGib)
	var minDiskSizeRequired int64 = gibToBytes(v.MinDiskSizeGib)

	switch host.Role {
	case "master":
		minCpuCoresRequired = v.MinCPUCoresMaster
		minRamRequired = gibToBytes(v.MinRamGibMaster)
	case "worker":
		minCpuCoresRequired = v.MinCPUCoresWorker
		minRamRequired = gibToBytes(v.MinRamGibWorker)
	}

	if hwInfo.CPU.Count < minCpuCoresRequired {
		reason += fmt.Sprintf(", insufficient CPU cores, expected: <%d> got <%d>", minCpuCoresRequired, hwInfo.CPU.Count)
	}

	if hwInfo.Memory.PhysicalBytes < minRamRequired {
		reason += fmt.Sprintf(", insufficient RAM requirements, expected: <%s> got <%s>",
			units.Base2Bytes(minRamRequired), units.Base2Bytes(hwInfo.Memory.PhysicalBytes))
	}

	if disks := listValidDisks(hwInfo, minDiskSizeRequired); len(disks) < 1 {
		reason += fmt.Sprintf(", insufficient number of disks with required size, "+
			"expected at least 1 not removable, not readonly disk of size more than <%d>", minDiskSizeRequired)
	}

	if len(reason) == 0 {
		isSufficient = true
	} else {
		reason = fmt.Sprintf("host has insufficient hardware%s", reason)
		if host.Role != "" {
			reason = fmt.Sprintf("%s %s", host.Role, reason)
		}
	}

	return &IsSufficientReply{
		IsSufficient: isSufficient,
		Reason:       reason,
	}, nil
}

func (v *validator) GetHostValidDisks(host *models.Host) ([]*models.Disk, error) {
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return nil, err
	}
	disks := listValidDisks(inventory, gibToBytes(v.MinDiskSizeGib))
	if len(disks) == 0 {
		return nil, fmt.Errorf("host %s doesn't have valid disks", host.ID)
	}
	return disks, nil
}

func gibToBytes(gib int64) int64 {
	return gib * int64(units.GiB)
}

func listValidDisks(inventory models.Inventory, minSizeRequiredInBytes int64) []*models.Disk {
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
