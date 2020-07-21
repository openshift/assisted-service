package host

import (
	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/sirupsen/logrus"
)

type validationResult struct {
	ID      validationID     `json:"id"`
	Status  validationStatus `json:"status"`
	Message string           `json:"message"`
}

type refreshPreprocessor struct {
	log         logrus.FieldLogger
	validations []validation
}

func newRefreshPreprocessor(log logrus.FieldLogger, hwValidatorCfg *hardware.ValidatorCfg) *refreshPreprocessor {
	return &refreshPreprocessor{
		log:         log,
		validations: newValidations(log, hwValidatorCfg),
	}
}

func (r *refreshPreprocessor) preprocess(c *validationContext) (map[validationID]bool, map[string][]validationResult, error) {
	stateMachineInput := make(map[validationID]bool)
	validationsOutput := make(map[string][]validationResult)
	for _, v := range r.validations {
		st := v.condition(c)
		stateMachineInput[v.id] = st == ValidationSuccess
		message := v.formatter(c, st)
		category, err := v.id.category()
		if err != nil {
			logrus.WithError(err).Warn("id.category()")
			return nil, nil, err
		}
		validationsOutput[category] = append(validationsOutput[category], validationResult{
			ID:      v.id,
			Status:  st,
			Message: message,
		})
	}
	return stateMachineInput, validationsOutput, nil
}

func newValidations(log logrus.FieldLogger, hwValidatorCfg *hardware.ValidatorCfg) []validation {
	v := validator{
		log:            log,
		hwValidatorCfg: hwValidatorCfg,
	}
	ret := []validation{
		{
			id:        IsConnected,
			condition: v.isConnected,
			formatter: v.printConnected,
		},
		{
			id:        HasInventory,
			condition: v.hasInventory,
			formatter: v.printHasInventory,
		},
		{
			id:        HasMinCPUCores,
			condition: v.hasMinCpuCores,
			formatter: v.printHasMinCpuCores,
		},
		{
			id:        HasMinMemory,
			condition: v.hasMinMemory,
			formatter: v.printHasMinMemory,
		},
		{
			id:        HasMinValidDisks,
			condition: v.hasMinValidDisks,
			formatter: v.printHasMinValidDisks,
		},
		{
			id:        IsMachineCidrDefined,
			condition: v.isMachineCidrDefined,
			formatter: v.printIsMachineCidrDefined,
		},
		{
			id:        IsRoleDefined,
			condition: v.isRoleDefined,
			formatter: v.printIsRoleDefined,
		},
		{
			id:        HasCPUCoresForRole,
			condition: v.hasCpuCoresForRole,
			formatter: v.printHasCpuCoresForRole,
		},
		{
			id:        HasMemoryForRole,
			condition: v.hasMemoryForRole,
			formatter: v.printHasMemoryForRole,
		},
		{
			id:        IsHostnameUnique,
			condition: v.isHostnameUnique,
			formatter: v.printHostnameUnique,
		},
		{
			id:        BelongsToMachineCidr,
			condition: v.belongsToMachineCidr,
			formatter: v.printBelongsToMachineCidr,
		},
		{
			id:        IsHostnameValid,
			condition: v.isHostnameValid,
			formatter: v.printHostnameValid,
		},
	}
	return ret
}
