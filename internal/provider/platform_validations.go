package provider

import (
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

// merge the current platform set in cluster with the platform given in input
func mergePlatforms(platform *models.Platform, cluster *common.Cluster) *models.Platform {
	if cluster == nil {
		return platform
	}

	var mergedPlatform models.Platform

	mergedPlatform.Type = platform.Type
	if mergedPlatform.Type == nil {
		mergedPlatform.Type = cluster.Platform.Type
	}

	if platform.External == nil {
		mergedPlatform.External = cluster.Platform.External
		return &mergedPlatform
	}

	mergedPlatform.External = &models.PlatformExternal{
		PlatformName:           platform.External.PlatformName,
		CloudControllerManager: platform.External.CloudControllerManager,
	}

	if cluster.Platform.External == nil {
		// existing cluster has no external setting set, nothing else do to
		return &mergedPlatform
	}

	if mergedPlatform.External.PlatformName == nil {
		mergedPlatform.External.PlatformName = cluster.Platform.External.PlatformName
	}

	if mergedPlatform.External.CloudControllerManager == nil {
		mergedPlatform.External.CloudControllerManager = cluster.Platform.External.CloudControllerManager
	}

	return &mergedPlatform
}

func validateOciExternalIntegration(platform *models.Platform) error {
	if !common.IsOciExternalIntegrationEnabled(platform) {
		return nil
	}

	if platform.External.CloudControllerManager == nil ||
		*platform.External.CloudControllerManager != models.PlatformExternalCloudControllerManagerExternal {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Cloud controller manager must be enabled when using oci external integration"))
	}

	return nil
}

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

	var err error

	if cluster == nil || *cluster.Platform.Type != models.PlatformTypeExternal { // No existing cluster or existing cluster is not external
		if platform.Type != nil && *platform.Type == models.PlatformTypeExternal {
			// We require valid external settings when platform is set to external
			err = validateExternalSettingsForCreate(*platform)
		} else if areExternalSettingsSet(*platform) {
			// external settings shouldn't be set if platform type is not external
			err = common.NewApiError(http.StatusBadRequest, errors.Errorf("External settings can only be set with external platform type"))
		}
	} else if cluster != nil &&
		*cluster.Platform.Type == models.PlatformTypeExternal &&
		platform.Type != nil &&
		*platform.Type != models.PlatformTypeExternal &&
		areExternalSettingsSet(*platform) {
		// external setting shouldn't be set if platform type is not external
		err = common.NewApiError(http.StatusBadRequest, errors.Errorf("External settings can only be set with external platform type"))
	}

	if err != nil {
		return err
	}

	mergedPlatform := mergePlatforms(platform, cluster)
	err = validateOciExternalIntegration(mergedPlatform)
	if err != nil {
		return err
	}

	return nil
}
