package host

import (
	"context"
	"sort"
	"strings"

	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/sirupsen/logrus"
)

type stringer interface {
	String() string
}

type validationResult struct {
	ID      validationID     `json:"id"`
	Status  ValidationStatus `json:"status"`
	Message string           `json:"message"`
}

type validationsStatus map[string][]validationResult

type validationResults []validationResult

type refreshPreprocessor struct {
	log          logrus.FieldLogger
	validations  []validation
	conditions   []condition
	operatorsApi operators.API
}

func newRefreshPreprocessor(log logrus.FieldLogger, hwValidatorCfg *hardware.ValidatorCfg, hwValidator hardware.Validator, operatorsApi operators.API) *refreshPreprocessor {
	v := &validator{
		log:            log,
		hwValidatorCfg: hwValidatorCfg,
		hwValidator:    hwValidator,
		operatorsAPI:   operatorsApi,
	}
	return &refreshPreprocessor{
		log:          log,
		validations:  newValidations(v),
		conditions:   newConditions(v),
		operatorsApi: operatorsApi,
	}
}

func (r *refreshPreprocessor) preprocess(c *validationContext) (map[string]bool, validationsStatus, error) {
	stateMachineInput := make(map[string]bool)
	validationsOutput := make(validationsStatus)
	for _, v := range r.validations {
		st := v.condition(c)
		stateMachineInput[v.id.String()] = st == ValidationSuccess
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

	for _, cn := range r.conditions {
		stateMachineInput[cn.id.String()] = cn.fn(c)
	}

	// Validate operators
	results, err := r.operatorsApi.ValidateHost(context.TODO(), c.cluster, c.host)
	if err != nil {
		return nil, nil, err
	}
	for _, result := range results {
		id := validationID(result.ValidationId)
		stateMachineInput[id.String()] = result.Status == api.Success
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
		sortByValidationResultID(validationsOutput[category])
	}

	return stateMachineInput, validationsOutput, nil
}

// sortByValidationResultID sorts results by models.HostValidationID
func sortByValidationResultID(validationResults []validationResult) {
	sort.SliceStable(validationResults, func(i, j int) bool {
		return validationResults[i].ID < validationResults[j].ID
	})
}

func newValidations(v *validator) []validation {
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
			condition: v.hasCPUCoresForRole,
			formatter: v.printHasCPUCoresForRole,
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
		{
			id:        SufficientOrUnknownInstallationDiskSpeed,
			condition: v.sufficientOrUnknownInstallationDiskSpeed,
			formatter: v.printSufficientOrUnknownInstallationDiskSpeed,
		},
	}
	return ret
}

func newConditions(v *validator) []condition {
	ret := []condition{
		{
			id: InstallationDiskSpeedCheckSuccessful,
			fn: v.isInstallationDiskSpeedCheckSuccessful,
		},
		{
			id: ClusterPreparingForInstallation,
			fn: v.isClusterPreparingForInstallation,
		},
		{
			id: ClusterInstalling,
			fn: v.isClusterInstalling,
		},
		{
			id: ValidRoleForInstallation,
			fn: v.isValidRoleForInstallation,
		},
	}
	return ret
}
