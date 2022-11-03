package host

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/models"
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

func newRefreshPreprocessor(log logrus.FieldLogger, hwValidatorCfg *hardware.ValidatorCfg, hwValidator hardware.Validator,
	operatorsApi operators.API, disabledHostValidations DisabledHostValidations, providerRegistry registry.ProviderRegistry) *refreshPreprocessor {
	v := &validator{
		log:              log,
		hwValidatorCfg:   hwValidatorCfg,
		hwValidator:      hwValidator,
		operatorsAPI:     operatorsApi,
		providerRegistry: providerRegistry,
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

func (r *refreshPreprocessor) preprocess(ctx context.Context, c *validationContext) (map[string]bool, ValidationsStatus, error) {
	conditions := make(map[string]bool)
	validationsOutput := make(ValidationsStatus)
	for _, v := range r.validations {

		if err := ctx.Err(); err != nil {
			logrus.WithError(err).Warn("context error")
			return nil, nil, fmt.Errorf("context error: %w", err)
		}

		var st ValidationStatus
		var message string
		if r.disabledHostValidations.IsDisabled(v.id) {
			st = ValidationDisabled
			message = validationDisabledByConfiguration
			conditions[v.id.String()] = true
		} else {
			st, message = v.condition(c)
			conditions[v.id.String()] = funk.ContainsString([]string{ValidationSuccess.String(), ValidationSuccessSuppressOutput.String()}, st.String())
			// Don't output this validation status to validations in case that the output needs to be suppressed
			if st == ValidationSuccessSuppressOutput {
				continue
			}
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

	if c.infraEnv == nil {
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
			skippedStates: manualRebootStages,
		},
		{
			id:            IsMediaConnected,
			condition:     v.isMediaConnected,
			skippedStates: manualRebootStages,
		},
		{
			id:        HasInventory,
			condition: v.hasInventory,
		},
		{
			id:        HasMinCPUCores,
			condition: v.hasMinCpuCores,
		},
		{
			id:        HasMinMemory,
			condition: v.hasMinMemory,
		},
		{
			id:        HasMinValidDisks,
			condition: v.hasMinValidDisks,
		},
		{
			id:        IsMachineCidrDefined,
			condition: v.isMachineCidrDefined,
		},
		{
			id:        HasCPUCoresForRole,
			condition: v.hasCPUCoresForRole,
		},
		{
			id:        HasMemoryForRole,
			condition: v.hasMemoryForRole,
		},
		{
			id:        IsHostnameUnique,
			condition: v.isHostnameUnique,
		},
		{
			id:        BelongsToMachineCidr,
			condition: v.belongsToMachineCidr,
		},
		{
			id:        IsHostnameValid,
			condition: v.isHostnameValid,
		},
		{
			id:        IsIgnitionDownloadable,
			condition: v.isIgnitionDownloadable,
		},
		{
			id:        BelongsToMajorityGroup,
			condition: v.belongsToMajorityGroup,
		},
		{
			id:        IsPlatformNetworkSettingsValid,
			condition: v.isValidPlatformNetworkSettings,
		},
		{
			id:            IsNTPSynced,
			condition:     v.isNTPSynced,
			skippedStates: manualRebootStages,
		},
		{
			id:            IsTimeSyncedBetweenHostAndService,
			condition:     v.isTimeSyncedBetweenHostAndService,
			skippedStates: manualRebootStages,
		},
		{
			id:        SucessfullOrUnknownContainerImagesAvailability,
			condition: v.sucessfullOrUnknownContainerImagesAvailability,
		},
		{
			id:        SufficientOrUnknownInstallationDiskSpeed,
			condition: v.sufficientOrUnknownInstallationDiskSpeed,
		},
		{
			id:        HasSufficientNetworkLatencyRequirementForRole,
			condition: v.hasSufficientNetworkLatencyRequirementForRole,
		}, {
			id:        HasSufficientPacketLossRequirementForRole,
			condition: v.hasSufficientPacketLossRequirementForRole,
		},
		{
			id:        HasDefaultRoute,
			condition: v.hasDefaultRoute,
		},
		{
			id:        IsAPIDomainNameResolvedCorrectly,
			condition: v.isAPIDomainNameResolvedCorrectly,
		},
		{
			id:        IsAPIInternalDomainNameResolvedCorrectly,
			condition: v.isAPIInternalDomainNameResolvedCorrectly,
		},
		{
			id:        IsAppsDomainNameResolvedCorrectly,
			condition: v.isAppsDomainNameResolvedCorrectly,
		},
		{
			id:        CompatibleWithClusterPlatform,
			condition: v.compatibleWithClusterPlatform,
		},
		{
			id:        IsDNSWildcardNotConfigured,
			condition: v.isDNSWildcardNotConfigured,
		},
		{
			id:        DiskEncryptionRequirementsSatisfied,
			condition: v.diskEncryptionRequirementsSatisfied,
		},
		{
			id:        NonOverlappingSubnets,
			condition: v.nonOverlappingSubnets,
		},
		{
			id:        VSphereHostUUIDEnabled,
			condition: v.isVSphereDiskUUIDEnabled,
		},
		{
			id:        CompatibleAgent,
			condition: v.compatibleAgent,
		},
		{
			id:        NoSkipInstallationDisk,
			condition: v.noSkipInstallationDisk,
		},
		{
			id:        NoSkipMissingDisk,
			condition: v.noSkipMissingDisk,
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
