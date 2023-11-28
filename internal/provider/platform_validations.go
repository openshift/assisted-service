package provider

import (
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

func validateExternalPlatformName(platformName *string) bool {
	return platformName != nil && len(*platformName) > 0
}

func validateExternalCloudControllerManager(cloudControllerManager string) bool {
	validCCMvalues := []string{models.PlatformExternalCloudControllerManagerEmpty, models.PlatformExternalCloudControllerManagerExternal}
	return funk.Contains(validCCMvalues, cloudControllerManager)
}

func validateExternalSettingsForCreate(platform models.Platform) error {
	if platform.External == nil {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("External setting must be set when using platform type external"))
	}

	if !validateExternalPlatformName(platform.External.PlatformName) {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Platform name must be set to a non-empty string when using platform type external"))
	}

	if platform.External.CloudControllerManager != nil && !validateExternalCloudControllerManager(*platform.External.CloudControllerManager) {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Cloud controller manager value can only be set to \"\" or \"External\" when using platform type external"))
	}

	return nil
}

func validateExternalSettingsForUpdate(platform models.Platform) error {
	if platform.External == nil {
		return nil
	}

	if platform.External.PlatformName != nil && !validateExternalPlatformName(platform.External.PlatformName) {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Platform name must be set to a non-empty string when using platform type external"))
	}

	if platform.External.CloudControllerManager != nil && !validateExternalCloudControllerManager(*platform.External.CloudControllerManager) {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Cloud controller manager value can only be set to \"\" or \"External\" when using platform type external"))
	}

	return nil
}

func validateExternalPlatform(platform *models.Platform, cluster *common.Cluster) error {
	// No existing cluster or existing cluster is not external.
	if platform != nil && (cluster == nil || *cluster.Platform.Type != models.PlatformTypeExternal) {
		if platform.Type != nil && *platform.Type == models.PlatformTypeExternal {
			// We require valid external settings when platform is set to external
			return validateExternalSettingsForCreate(*platform)
		} else if areExternalSettingsSet(*platform) {
			// external setting shouldn't be set if platform type is not external
			return common.NewApiError(http.StatusBadRequest, errors.Errorf("External settings can only be set with external platform type"))
		}
	}

	// Existing cluster is external
	if platform != nil && cluster != nil && *cluster.Platform.Type == models.PlatformTypeExternal {
		if platform.Type == nil || *platform.Type == models.PlatformTypeExternal {
			// validate external settings passed in platform object
			return validateExternalSettingsForUpdate(*platform)
		} else if areExternalSettingsSet(*platform) {
			// external setting shouldn't be set if platform type is not external
			return common.NewApiError(http.StatusBadRequest, errors.Errorf("External settings can only be set with external platform type"))
		}
	}

	return nil
}
