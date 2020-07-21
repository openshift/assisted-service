package host

import (
	"net/http"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/pkg/errors"

	"github.com/filanov/bm-inventory/models"
)

type validationID models.HostValidationID

const (
	IsConnected          = validationID(models.HostValidationIDConnected)
	HasInventory         = validationID(models.HostValidationIDHasInventory)
	IsMachineCidrDefined = validationID(models.HostValidationIDMachineCidrDefined)
	BelongsToMachineCidr = validationID(models.HostValidationIDBelongsToMachineCidr)
	HasMinCPUCores       = validationID(models.HostValidationIDHasMinCPUCores)
	HasMinValidDisks     = validationID(models.HostValidationIDHasMinValidDisks)
	HasMinMemory         = validationID(models.HostValidationIDHasMinMemory)
	HasCPUCoresForRole   = validationID(models.HostValidationIDHasCPUCoresForRole)
	HasMemoryForRole     = validationID(models.HostValidationIDHasMemoryForRole)
	IsHostnameUnique     = validationID(models.HostValidationIDHostnameUnique)
	IsRoleDefined        = validationID(models.HostValidationIDRoleDefined)
	IsHostnameValid      = validationID(models.HostValidationIDHostnameValid)
)

func (v validationID) category() (string, error) {
	switch v {
	case IsConnected, IsMachineCidrDefined, BelongsToMachineCidr:
		return "network", nil
	case HasInventory, HasMinCPUCores, HasMinValidDisks, HasMinMemory,
		HasCPUCoresForRole, HasMemoryForRole, IsHostnameUnique, IsHostnameValid:
		return "hardware", nil
	case IsRoleDefined:
		return "role", nil
	}
	return "", common.NewApiError(http.StatusInternalServerError, errors.Errorf("Unexpected validation id %s", string(v)))
}

func (v validationID) String() string {
	return string(v)
}
