package host

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/openshift/assisted-service/internal/hardware"
	internalmodels "github.com/openshift/assisted-service/internal/models"
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

func (r *refreshPreprocessor) preprocess(c *validationContext) (map[string]bool, internalmodels.HostValidationsStatus, error) {
	conditions := make(map[string]bool)
	validationsOutput := make(internalmodels.HostValidationsStatus)
	for _, v := range r.validations {

		var st internalmodels.HostValidationStatus
		var message string
		if r.disabledHostValidations.IsDisabled(v.id) {
			st = internalmodels.HostValidationDisabled
			message = validationDisabledByConfiguration
			conditions[v.id.String()] = true
		} else {
			st = v.condition(c)
			conditions[v.id.String()] = funk.ContainsString([]string{internalmodels.HostValidationSuccess.String(), internalmodels.HostValidationSuccessSuppressOutput.String()}, st.String())
			// Don't output this validation status to validations in case that the output needs to be suppressed
			if st == internalmodels.HostValidationSuccessSuppressOutput {
				continue
			}
			message = v.formatter(c, st)
		}

		// skip the validations per states
		if funk.Contains(v.skippedStates, c.host.Progress.CurrentStage) {
			continue
		}
		category, err := v.id.Category()
		if err != nil {
			logrus.WithError(err).Warn("id.Category()")
			return nil, nil, err
		}
		validationsOutput[category] = append(validationsOutput[category], internalmodels.HostValidationResult{
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
			id := internalmodels.HostValidationID(result.ValidationId)
			conditions[id.String()] = result.Status == api.Success
			category, err := id.Category()
			if err != nil {
				logrus.WithError(err).Warn("id.Category()")
				return nil, nil, err
			}

			status := internalmodels.HostValidationStatus(result.Status)

			validationsOutput[category] = append(validationsOutput[category], internalmodels.HostValidationResult{
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
func sortByValidationResultID(validationResults []internalmodels.HostValidationResult) {
	sort.SliceStable(validationResults, func(i, j int) bool {
		return validationResults[i].ID < validationResults[j].ID
	})
}

func newValidations(v *validator) []validation {
	return []validation{
		{
			id:            internalmodels.IsConnected,
			condition:     v.isConnected,
			formatter:     v.printConnected,
			skippedStates: manualRebootStages,
		},
		{
			id:            internalmodels.IsMediaConnected,
			condition:     v.isMediaConnected,
			formatter:     v.printMediaConnected,
			skippedStates: manualRebootStages,
		},
		{
			id:        internalmodels.HasInventory,
			condition: v.hasInventory,
			formatter: v.printHasInventory,
		},
		{
			id:        internalmodels.HasMinCPUCores,
			condition: v.hasMinCpuCores,
			formatter: v.printHasMinCpuCores,
		},
		{
			id:        internalmodels.HasMinMemory,
			condition: v.hasMinMemory,
			formatter: v.printHasMinMemory,
		},
		{
			id:        internalmodels.HasMinValidDisks,
			condition: v.hasMinValidDisks,
			formatter: v.printHasMinValidDisks,
		},
		{
			id:        internalmodels.IsMachineCidrDefined,
			condition: v.isMachineCidrDefined,
			formatter: v.printIsMachineCidrDefined,
		},
		{
			id:        internalmodels.HasCPUCoresForRole,
			condition: v.hasCPUCoresForRole,
			formatter: v.printHasCPUCoresForRole,
		},
		{
			id:        internalmodels.HasMemoryForRole,
			condition: v.hasMemoryForRole,
			formatter: v.printHasMemoryForRole,
		},
		{
			id:        internalmodels.IsHostnameUnique,
			condition: v.isHostnameUnique,
			formatter: v.printHostnameUnique,
		},
		{
			id:        internalmodels.BelongsToMachineCidr,
			condition: v.belongsToMachineCidr,
			formatter: v.printBelongsToMachineCidr,
		},
		{
			id:        internalmodels.IsHostnameValid,
			condition: v.isHostnameValid,
			formatter: v.printHostnameValid,
		},
		{
			id:        internalmodels.IsIgnitionDownloadable,
			condition: v.isIgnitionDownloadable,
			formatter: v.printIgnitionDownloadable,
		},
		{
			id:        internalmodels.BelongsToMajorityGroup,
			condition: v.belongsToMajorityGroup,
			formatter: v.printBelongsToMajorityGroup,
		},
		{
			id:        internalmodels.IsPlatformNetworkSettingsValid,
			condition: v.isValidPlatformNetworkSettings,
			formatter: v.printValidPlatformNetworkSettings,
		},
		{
			id:            internalmodels.IsNTPSynced,
			condition:     v.isNTPSynced,
			formatter:     v.printNTPSynced,
			skippedStates: manualRebootStages,
		},
		{
			id:        internalmodels.SucessfullOrUnknownContainerImagesAvailability,
			condition: v.sucessfullOrUnknownContainerImagesAvailability,
			formatter: v.printSucessfullOrUnknownContainerImagesAvailability,
		},
		{
			id:        internalmodels.SufficientOrUnknownInstallationDiskSpeed,
			condition: v.sufficientOrUnknownInstallationDiskSpeed,
			formatter: v.printSufficientOrUnknownInstallationDiskSpeed,
		},
		{
			id:        internalmodels.HasSufficientNetworkLatencyRequirementForRole,
			condition: v.hasSufficientNetworkLatencyRequirementForRole,
			formatter: v.printSufficientNetworkLatencyRequirementForRole,
		}, {
			id:        internalmodels.HasSufficientPacketLossRequirementForRole,
			condition: v.hasSufficientPacketLossRequirementForRole,
			formatter: v.printSufficientPacketLossRequirementForRole,
		},
		{
			id:        internalmodels.HasDefaultRoute,
			condition: v.hasDefaultRoute,
			formatter: v.printDefaultRoute,
		},
		{
			id:        internalmodels.IsAPIDomainNameResolvedCorrectly,
			condition: v.isAPIDomainNameResolvedCorrectly,
			formatter: v.printIsAPIDomainNameResolvedCorrectly,
		},
		{
			id:        internalmodels.IsAPIInternalDomainNameResolvedCorrectly,
			condition: v.isAPIInternalDomainNameResolvedCorrectly,
			formatter: v.printIsAPIInternalDomainNameResolvedCorrectly,
		},
		{
			id:        internalmodels.IsAppsDomainNameResolvedCorrectly,
			condition: v.isAppsDomainNameResolvedCorrectly,
			formatter: v.printIsAppsDomainNameResolvedCorrectly,
		},
		{
			id:        internalmodels.CompatibleWithClusterPlatform,
			condition: v.compatibleWithClusterPlatform,
			formatter: v.printCompatibleWithClusterPlatform,
		},
		{
			id:        internalmodels.IsDNSWildcardNotConfigured,
			condition: v.isDNSWildcardNotConfigured,
			formatter: v.printIsDNSWildcardNotConfigured,
		},
		{
			id:        internalmodels.DiskEncryptionRequirementsSatisfied,
			condition: v.diskEncryptionRequirementsSatisfied,
			formatter: v.printDiskEncryptionRequirementsSatisfied,
		},
		{
			id:        internalmodels.NonOverlappingSubnets,
			condition: v.nonOverlappingSubnets,
			formatter: v.printNonOverlappingSubnets,
		},
		{
			id:        internalmodels.VSphereHostUUIDEnabled,
			condition: v.isVSphereDiskUUIDEnabled,
			formatter: v.printVSphereUUIDEnabled,
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

func GetValidations(h *models.Host) (internalmodels.HostValidationsStatus, error) {
	var currentValidationRes internalmodels.HostValidationsStatus
	if h.ValidationsInfo != "" {
		if err := json.Unmarshal([]byte(h.ValidationsInfo), &currentValidationRes); err != nil {
			return internalmodels.HostValidationsStatus{}, errors.Wrapf(err, "Failed to unmarshal validations info from host %s in cluster %s", h.ID, h.ClusterID)
		}
	}
	return currentValidationRes, nil
}
