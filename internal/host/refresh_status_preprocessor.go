package host

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	models "github.com/openshift/assisted-service/models/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

type stringer interface {
	String() string
}

type ValidationResult struct {
	ID      validationID     `json:"id"`
	Status  ValidationStatus `json:"status"`
	Message string           `json:"message"`
}

type ValidationsStatus map[string]ValidationResults

type ValidationResults []ValidationResult

type refreshPreprocessor struct {
	log                     logrus.FieldLogger
	validations             []validation
	conditions              []condition
	operatorsApi            operators.API
	disabledHostValidations DisabledHostValidations
}

func newRefreshPreprocessor(log logrus.FieldLogger, hwValidatorCfg *hardware.ValidatorCfg, hwValidator hardware.Validator, operatorsApi operators.API, disabledHostValidations DisabledHostValidations) *refreshPreprocessor {
	v := &validator{
		log:            log,
		hwValidatorCfg: hwValidatorCfg,
		hwValidator:    hwValidator,
		operatorsAPI:   operatorsApi,
	}
	return &refreshPreprocessor{
		log:                     log,
		validations:             newValidations(v),
		conditions:              newConditions(v),
		operatorsApi:            operatorsApi,
		disabledHostValidations: disabledHostValidations,
	}
}

const validationDisabledByConfiguration = "Validation disabled by configuration"

func (r *refreshPreprocessor) preprocess(c *validationContext) (map[string]bool, ValidationsStatus, error) {
	conditions := make(map[string]bool)
	validationsOutput := make(ValidationsStatus)
	for _, v := range r.validations {

		var st ValidationStatus
		var message string
		if r.disabledHostValidations.IsDisabled(v.id) {
			st = ValidationDisabled
			message = validationDisabledByConfiguration
			conditions[v.id.String()] = true
		} else {
			st = v.condition(c)
			conditions[v.id.String()] = funk.ContainsString([]string{ValidationSuccess.String(), ValidationSuccessSuppressOutput.String()}, st.String())
			// Don't output this validation status to validations in case that the output needs to be suppressed
			if st == ValidationSuccessSuppressOutput {
				continue
			}
			message = v.formatter(c, st)
		}

		// skip the validations per states
		if funk.Contains(v.skippedStates, c.host.Progress.CurrentStage) {
			continue
		}
		category, err := v.id.category()
		if err != nil {
			logrus.WithError(err).Warn("id.category()")
			return nil, nil, err
		}
		validationsOutput[category] = append(validationsOutput[category], ValidationResult{
			ID:      v.id,
			Status:  st,
			Message: message,
		})
	}

	for _, cn := range r.conditions {
		conditions[cn.id.String()] = cn.fn(c)
	}

	// Validate operators
	results, err := r.operatorsApi.ValidateHost(context.TODO(), c.cluster, c.host)
	if err != nil {
		return nil, nil, err
	}
	for _, result := range results {
		id := validationID(result.ValidationId)
		conditions[id.String()] = result.Status == api.Success
		category, err := id.category()
		if err != nil {
			logrus.WithError(err).Warn("id.category()")
			return nil, nil, err
		}

		status := ValidationStatus(result.Status)

		validationsOutput[category] = append(validationsOutput[category], ValidationResult{
			ID:      id,
			Status:  status,
			Message: strings.Join(result.Reasons, "\n"),
		})
		sortByValidationResultID(validationsOutput[category])
	}

	return conditions, validationsOutput, nil
}

// sortByValidationResultID sorts results by models.HostValidationID
func sortByValidationResultID(validationResults []ValidationResult) {
	sort.SliceStable(validationResults, func(i, j int) bool {
		return validationResults[i].ID < validationResults[j].ID
	})
}

func newValidations(v *validator) []validation {
	return []validation{
		{
			id:            IsConnected,
			condition:     v.isConnected,
			formatter:     v.printConnected,
			skippedStates: manualRebootStages,
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
			id:            IsNTPSynced,
			condition:     v.isNTPSynced,
			formatter:     v.printNTPSynced,
			skippedStates: manualRebootStages,
		},
		{
			id:        SucessfullOrUnknownContainerImagesAvailability,
			condition: v.sucessfullOrUnknownContainerImagesAvailability,
			formatter: v.printSucessfullOrUnknownContainerImagesAvailability,
		},
		{
			id:        SufficientOrUnknownInstallationDiskSpeed,
			condition: v.sufficientOrUnknownInstallationDiskSpeed,
			formatter: v.printSufficientOrUnknownInstallationDiskSpeed,
		},
		{
			id:        HasSufficientNetworkLatencyRequirementForRole,
			condition: v.hasSufficientNetworkLatencyRequirementForRole,
			formatter: v.printSufficientNetworkLatencyRequirementForRole,
		}, {
			id:        HasSufficientPacketLossRequirementForRole,
			condition: v.hasSufficientPacketLossRequirementForRole,
			formatter: v.printSufficientPacketLossRequirementForRole,
		},
		{
			id:        HasDefaultRoute,
			condition: v.hasDefaultRoute,
			formatter: v.printDefaultRoute,
		},
	}
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
		{
			id: StageInWrongBootStages,
			fn: v.isStageInWrongBootStages,
		},
		{
			id: ClusterPendingUserAction,
			fn: v.isClusterPendingUserAction,
		},
		{
			id: ClusterInError,
			fn: v.isClusterInError,
		},
		{
			id: SuccessfulContainerImageAvailability,
			fn: v.isSuccessfulContainerImageAvailability,
		},
	}
	return ret
}

func GetValidations(h *models.Host) (ValidationsStatus, error) {
	var currentValidationRes ValidationsStatus
	if h.ValidationsInfo != "" {
		if err := json.Unmarshal([]byte(h.ValidationsInfo), &currentValidationRes); err != nil {
			return ValidationsStatus{}, errors.Wrapf(err, "Failed to unmarshal validations info from host %s in cluster %s", h.ID, h.ClusterID)
		}
	}
	return currentValidationRes, nil
}
