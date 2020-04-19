package hardware

import (
	"encoding/json"
	"fmt"

	"github.com/alecthomas/units"
	"github.com/filanov/bm-inventory/models"
)

type IsSufficientReply struct {
	IsSufficient bool
	Reason       string
}

//go:generate mockgen -source=validator.go -package=hardware -destination=mock_validator.go
type Validator interface {
	IsSufficient(host *models.Host) (*IsSufficientReply, error)
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
}

type validator struct {
	ValidatorCfg
}

func (v *validator) IsSufficient(host *models.Host) (*IsSufficientReply, error) {
	var err error
	var reason string
	var isSufficient bool
	var hwInfo models.Introspection

	if err = json.Unmarshal([]byte(host.HardwareInfo), &hwInfo); err != nil {
		return nil, err
	}

	var minCpuCoresRequired int64 = v.MinCPUCores
	var minRamRequired int64 = gibToBytes(v.MinRamGib)

	switch host.Role {
	case "master":
		minCpuCoresRequired = v.MinCPUCoresMaster
		minRamRequired = gibToBytes(v.MinRamGibMaster)
	case "worker":
		minCpuCoresRequired = v.MinCPUCoresWorker
		minRamRequired = gibToBytes(v.MinRamGibWorker)
	}

	if hwInfo.CPU.Cpus < minCpuCoresRequired {
		reason += fmt.Sprintf(", insufficient CPU cores, expected: <%d> got <%d>", minCpuCoresRequired, hwInfo.CPU.Cpus)
	}

	if total := getTotalMemory(hwInfo); total < minRamRequired {
		reason += fmt.Sprintf(", insufficient RAM requirements, expected: <%s> got <%s>",
			units.Base2Bytes(minRamRequired), units.Base2Bytes(total))
	}

	// TODO: check disk space

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

func getTotalMemory(hwInfo models.Introspection) int64 {
	for i := range hwInfo.Memory {
		if hwInfo.Memory[i].Name == "Mem" {
			return hwInfo.Memory[i].Total
		}
	}
	return 0
}

func gibToBytes(gib int64) int64 {
	return gib * int64(units.GiB)
}
