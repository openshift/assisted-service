package hardware

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/filanov/bm-inventory/internal/validators"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/internal/common"

	"github.com/alecthomas/units"
	"github.com/filanov/bm-inventory/models"
)

const diskNameFilterRegex = "nvme"
const localhost = "localhost"

//go:generate mockgen -source=validator.go -package=hardware -destination=mock_validator.go
type Validator interface {
	IsSufficient(host *models.Host, cluster *common.Cluster) (*validators.IsSufficientReply, error)
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
	MinDiskSizeGib    int64 `envconfig:"HW_VALIDATOR_MIN_DISK_SIZE_GIB" default:"120"`
}

type validator struct {
	ValidatorCfg
	log logrus.FieldLogger
}

func (v *validator) IsSufficient(host *models.Host, cluster *common.Cluster) (*validators.IsSufficientReply, error) {
	var err error
	var reasons []string
	var isSufficient bool
	var hwInfo models.Inventory

	if host.Inventory == "" {
		return &validators.IsSufficientReply{
			Type:         "hardware",
			IsSufficient: false,
			Reason:       "Waiting to receive inventory information",
		}, nil
	}

	if err = json.Unmarshal([]byte(host.Inventory), &hwInfo); err != nil {
		return nil, err
	}

	cluster.ID = &host.ClusterID

	var minCpuCoresRequired int64 = v.MinCPUCores
	var minRamRequired int64 = gibToBytes(v.MinRamGib)
	var minDiskSizeRequired int64 = gibToBytes(v.MinDiskSizeGib)

	switch host.Role {
	case models.HostRoleMaster:
		minCpuCoresRequired = v.MinCPUCoresMaster
		minRamRequired = gibToBytes(v.MinRamGibMaster)
	case models.HostRoleWorker:
		minCpuCoresRequired = v.MinCPUCoresWorker
		minRamRequired = gibToBytes(v.MinRamGibWorker)
	}

	if hwInfo.CPU.Count < minCpuCoresRequired {
		reasons = append(reasons, fmt.Sprintf("insufficient CPU cores, expected: <%d> got <%d>", minCpuCoresRequired, hwInfo.CPU.Count))
	}

	if hwInfo.Memory.PhysicalBytes < minRamRequired {
		reasons = append(reasons, fmt.Sprintf("insufficient RAM requirements, expected: <%s> got <%s>",
			units.Base2Bytes(minRamRequired), units.Base2Bytes(hwInfo.Memory.PhysicalBytes)))
	}

	if disks := listValidDisks(hwInfo, minDiskSizeRequired); len(disks) < 1 {
		reasons = append(reasons, fmt.Sprintf("insufficient number of disks with required size, "+
			"expected at least 1 not removable, not readonly disk of size more than %d  bytes", minDiskSizeRequired))
	}

	valid, notValidReason := v.isHostnameValid(cluster, host, hwInfo.Hostname)
	if !valid {
		reasons = append(reasons, notValidReason)
	}

	var reason string
	if len(reasons) == 0 {
		isSufficient = true
	} else {
		reason = strings.Join(reasons[:], ",")
		if host.Role != "" {
			reason = fmt.Sprintf("%s %s", host.Role, reason)
		}
	}

	return &validators.IsSufficientReply{
		Type:         "hardware",
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

func (v *validator) isHostnameValid(cluster *common.Cluster, host *models.Host, hwInventoryHostname string) (bool, string) {
	hostToCheckHostname, err := common.GetCurrentHostName(host)
	if err != nil {
		return false, "failed to get hostname from hardware info"
	}
	if hostToCheckHostname == localhost {
		return false, "hostname \"localhost\" is forbidden"
	}

	for _, chost := range cluster.Hosts {
		if host.ID.String() == chost.ID.String() {
			continue
		}
		if chost.Inventory == "" || *chost.Status == models.HostStatusDisabled {
			continue
		}

		hostToCompareHostname, err := common.GetCurrentHostName(chost)
		if err != nil {
			continue
		}

		if hostToCheckHostname == hostToCompareHostname {
			return false, fmt.Sprintf("host with hostname \"%s\" already exists.", hostToCheckHostname)
		}
	}
	return true, ""
}
