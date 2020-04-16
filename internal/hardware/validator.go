package hardware

import (
	"encoding/json"
	"fmt"

	"github.com/alecthomas/units"
	"github.com/filanov/bm-inventory/models"
)

const (
	// minimal CPU requirements
	minCPUCores       = 2
	minCpuCoresMaster = 4
	minCpuCoresWorker = 2
	// minimal RAM requirements
	minRam       = int64(8 * units.Gibibyte)
	minRamMaster = int64(16 * units.Gibibyte)
	minRamWorker = int64(8 * units.Gibibyte)
)

type IsSufficientReply struct {
	IsSufficient bool
	Reason       string
}

//go:generate mockgen -source=validator.go -package=hardware -destination=mock_validator.go
type Validator interface {
	IsSufficient(host *models.Host) (*IsSufficientReply, error)
}

func NewValidator() Validator {
	return &validator{}
}

type validator struct{}

func (v *validator) IsSufficient(host *models.Host) (*IsSufficientReply, error) {
	var err error
	var reason string
	var isSufficient bool
	var hwInfo models.Introspection

	if err = json.Unmarshal([]byte(host.HardwareInfo), &hwInfo); err != nil {
		return nil, err
	}

	var minCpuCoresRequired int64 = minCPUCores
	var minRamRequired int64 = minRam

	switch host.Role {
	case "master":
		minCpuCoresRequired = minCpuCoresMaster
		minRamRequired = minRamMaster
	case "worker":
		minCpuCoresRequired = minCpuCoresWorker
		minRamRequired = minRamWorker
	}

	if hwInfo.CPU.Cpus < minCpuCoresRequired {
		reason += fmt.Sprintf("insufficient CPU cores, expected: <%d> got <%d>", minCpuCoresRequired, hwInfo.CPU.Cpus)
	}

	if total := sumMemory(hwInfo); total < minRamRequired {
		reason += fmt.Sprintf(", insufficient RAM requirements, expected: <%d> got <%d>", total, minRamRequired)
	}

	// TODO: check disk space

	if len(reason) == 0 {
		isSufficient = true
	} else {
		reason = fmt.Sprintf("host have insufficient hardware, %s", reason)
		if host.Role != "" {
			reason = fmt.Sprintf("%s %s", host.Role, reason)
		}
	}

	return &IsSufficientReply{
		IsSufficient: isSufficient,
		Reason:       reason,
	}, nil
}

func sumMemory(hwInfo models.Introspection) int64 {
	var sum int64
	for i := range hwInfo.Memory {
		if hwInfo.Memory[i].Name == "Mem" {
			sum += hwInfo.Memory[i].Total
		}
	}
	return sum
}
