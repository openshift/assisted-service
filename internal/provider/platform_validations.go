package provider

import (
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

func validateExternalSettingsForCreate(platform models.Platform) error {
	if platform.External == nil {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("External setting must be set when using platform type external"))
	}

	if platform.External.PlatformName == nil {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Platform name must be set to a non-empty string when using platform type external"))
	}

	return nil
}

func validateExternalPlatform(platform *models.Platform, cluster *common.Cluster) error {
	if platform == nil {
		// nothing to check
		return nil
	}

	// No existing cluster or existing cluster is not external.
	if cluster == nil || *cluster.Platform.Type != models.PlatformTypeExternal {
		if platform.Type != nil && *platform.Type == models.PlatformTypeExternal {
			// We require valid external settings when platform is set to external
			return validateExternalSettingsForCreate(*platform)
		} else if areExternalSettingsSet(*platform) {
			// external setting shouldn't be set if platform type is not external
			return common.NewApiError(http.StatusBadRequest, errors.Errorf("External settings can only be set with external platform type"))
		}
	}

	// Existing cluster is external and platform type is not external
	if cluster != nil &&
		*cluster.Platform.Type == models.PlatformTypeExternal &&
		platform.Type != nil &&
		*platform.Type != models.PlatformTypeExternal &&
		areExternalSettingsSet(*platform) {
		// external setting shouldn't be set if platform type is not external
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("External settings can only be set with external platform type"))
	}

	return nil
}
