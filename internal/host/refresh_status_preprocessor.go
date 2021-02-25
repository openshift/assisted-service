package host

import (
	"context"
	"strings"

	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/sirupsen/logrus"
)

type validationResult struct {
	ID      validationID     `json:"id"`
	Status  ValidationStatus `json:"status"`
	Message string           `json:"message"`
}

type refreshPreprocessor struct {
	log          logrus.FieldLogger
	validations  []validation
	operatorsApi operators.API
}

func newRefreshPreprocessor(log logrus.FieldLogger, hwValidatorCfg *hardware.ValidatorCfg, hwValidator hardware.Validator, operatorsApi operators.API) *refreshPreprocessor {
	return &refreshPreprocessor{
		log:          log,
		validations:  newValidations(log, hwValidatorCfg, hwValidator),
		operatorsApi: operatorsApi,
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

	// Validate operators
	results, err := r.operatorsApi.ValidateHost(context.TODO(), c.cluster, c.host)
	if err != nil {
		return nil, nil, err
	}
	for _, result := range results {
		id := validationID(result.ValidationId)
		stateMachineInput[id] = result.Status == api.Success
		category, err := id.category()
		if err != nil {
			logrus.WithError(err).Warn("id.category()")
			return nil, nil, err
		}

		status := ValidationStatus(result.Status)

		validationsOutput[category] = append(validationsOutput[category], validationResult{
			ID:      id,
			Status:  status,
			Message: strings.Join(result.Reasons, "\n"),
		})
	}

	return stateMachineInput, validationsOutput, nil
}

func newValidations(log logrus.FieldLogger, hwValidatorCfg *hardware.ValidatorCfg, hwValidator hardware.Validator) []validation {
	v := validator{
		log:            log,
		hwValidatorCfg: hwValidatorCfg,
		hwValidator:    hwValidator,
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
		{
			id:        IsAPIVipConnected,
			condition: v.isAPIVipConnected,
			formatter: v.printAPIVipConnected,
		},
		{
			id:        BelongsToMajorityGroup,
			condition: v.belongsToMajorityGroup,
			formatter: v.printBelongsToMajorityGroup,
		},
		{
			id:        IsPlatformValid,
			condition: v.isValidPlatform,
			formatter: v.printValidPlatform,
		},
		{
			id:        IsNTPSynced,
			condition: v.isNTPSynced,
			formatter: v.printNTPSynced,
		},
		{
			id:        AreContainerImagesAvailable,
			condition: v.areImagesAvailable,
			formatter: v.printImageAvailability,
		},
	}
	return ret
}
