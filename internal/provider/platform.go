package provider

import (
	"net/http"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

func isPlatformBM(platform *models.Platform) bool {
	return platform != nil && platform.Type != nil && *platform.Type == models.PlatformTypeBaremetal
}

func isPlatformNone(platform *models.Platform) bool {
	return platform != nil && platform.Type != nil && *platform.Type == models.PlatformTypeNone
}

func isClusterPlatformNone(cluster *common.Cluster) bool {
	return cluster != nil && isPlatformNone(cluster.Platform)
}

func isUMNAllowedForPlatform(platform *models.Platform) bool {
	if platform == nil || platform.Type == nil {
		return true
	}

	switch *platform.Type {
	case models.PlatformTypeBaremetal, models.PlatformTypeNutanix:
		return false
	}
	return true
}

func isUMNMandatoryForPlatform(platform *models.Platform) bool {
	if platform == nil || platform.Type == nil {
		return true
	}

	if *platform.Type == models.PlatformTypeNone || common.IsPlatformExternal(platform) {
		return true
	}

	return false
}

func isClusterPlatformBM(cluster *common.Cluster) bool {
	return cluster != nil && isPlatformBM(cluster.Platform)
}

func isUMNAllowedForCluster(cluster *common.Cluster) bool {
	return cluster != nil && isUMNAllowedForPlatform(cluster.Platform)
}

func isUMNMandatoryForCluster(cluster *common.Cluster) bool {
	return cluster != nil && isUMNMandatoryForPlatform(cluster.Platform)
}

// set default values for the external platform
func setExternalDefaultValues(platform *models.Platform, cluster *common.Cluster) {
	if platform == nil {
		// nothing to update
		return
	}

	if platform.Type != nil && *platform.Type != models.PlatformTypeExternal {
		// not an external platform
		return
	}

	if platform.Type == nil && cluster != nil && *cluster.Platform.Type != models.PlatformTypeExternal {
		// not an external platform
		return
	}

	// set platform type to force the update of the platform later in DB
	platform.Type = common.PlatformTypePtr(models.PlatformTypeExternal)

	// we are creating a new cluster, set CloudControllerManager to "" if unset
	if cluster == nil && platform.External.CloudControllerManager == nil {
		platform.External.CloudControllerManager = swag.String(models.PlatformExternalCloudControllerManagerEmpty)
	}

	// we are updating an existing cluster from a non-external cluster
	// we must ensure that CloudControllerManager is set
	if cluster != nil && *cluster.Platform.Type != models.PlatformTypeExternal && platform.External.CloudControllerManager == nil {
		platform.External.CloudControllerManager = swag.String(models.PlatformExternalCloudControllerManagerEmpty)
	}
}

func areExternalSettingsSet(platform models.Platform) bool {
	return platform.External != nil && (platform.External.PlatformName != nil || platform.External.CloudControllerManager != nil)
}

func checkPlatformWrongParamsInput(platform *models.Platform, userManagedNetworking *bool, cluster *common.Cluster) error {
	// check if platform compatibility with UMN
	if platform != nil && userManagedNetworking != nil {
		userManagedNetworkingStatus := "enabled"
		if !*userManagedNetworking {
			userManagedNetworkingStatus = "disabled"
		}
		if (!*userManagedNetworking && isUMNMandatoryForPlatform(platform)) || (*userManagedNetworking && !isUMNAllowedForPlatform(platform)) {
			return common.NewApiError(http.StatusBadRequest, errors.Errorf("Can't set %s platform with user-managed-networking %s", *platform.Type, userManagedNetworkingStatus))
		}
	}

	// If current cluster platform is different than baremetal/none, and we want to set the cluster platform to one
	// of those platforms, that might cause the cluster to be in wrong state (baremetal + umn enabled, none + umn disabled)
	// In those cases return bad request
	if userManagedNetworking == nil &&
		cluster != nil &&
		platform != nil &&
		swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeFull && // no need to check SNO, it will be validated later in the update/creation
		(!(isClusterPlatformBM(cluster) && isPlatformNone(platform)) &&
			!(isClusterPlatformNone(cluster) && isPlatformBM(platform))) {

		if !isUMNAllowedForPlatform(platform) && swag.BoolValue(cluster.UserManagedNetworking) {
			return common.NewApiError(http.StatusBadRequest, errors.Errorf("Can't set %s platform with user-managed-networking enabled", *platform.Type))
		}

		if isUMNMandatoryForPlatform(platform) && !swag.BoolValue(cluster.UserManagedNetworking) {
			return common.NewApiError(http.StatusBadRequest, errors.Errorf("Can't set %s platform with user-managed-networking disabled", *platform.Type))
		}
	}

	if err := validateExternalPlatform(platform, cluster); err != nil {
		return err
	}

	return nil
}

func doesPlatformAllowUMNOrCMN(platform *models.Platform, cluster *common.Cluster) bool {
	if platform != nil && isUMNAllowedForPlatform(platform) && !isUMNMandatoryForPlatform(platform) {
		return true
	}

	if platform == nil && isUMNAllowedForCluster(cluster) && !isUMNMandatoryForCluster(cluster) {
		return true
	}

	return false
}

func createPlatformFromType(platformType models.PlatformType) *models.Platform {
	platform := &models.Platform{
		Type: &platformType,
	}
	return updatePlatformIsExternal(platform)
}

func updatePlatformIsExternal(platform *models.Platform) *models.Platform {
	if platform == nil {
		return nil
	}
	platform.IsExternal = swag.Bool(common.IsPlatformExternal(platform))

	return platform
}

// checkExternalPlatformUpdate checks if external settings need to be updated, returns nil if no updates are required
func checkExternalPlatformUpdate(platform models.Platform, cluster *common.Cluster) *models.Platform {
	if platform.External == nil {
		return nil
	}

	update := false
	if platform.External.PlatformName != nil && *cluster.Platform.External.PlatformName != *platform.External.PlatformName {
		update = true
	}

	if platform.External.CloudControllerManager != nil && *cluster.Platform.External.CloudControllerManager != *platform.External.CloudControllerManager {
		update = true
	}

	if update {
		return updatePlatformIsExternal(&platform)
	}

	return nil
}

// checkPlatformUpdate check if platform needs to be updated, returns nil if no updates are required
func checkPlaformUpdate(platform *models.Platform, cluster *common.Cluster) *models.Platform {
	if platform == nil || platform.Type == nil {
		return nil
	}

	if *cluster.Platform.Type == *platform.Type && *cluster.Platform.Type == models.PlatformTypeExternal {
		return checkExternalPlatformUpdate(*platform, cluster)
	}

	if *cluster.Platform.Type == *platform.Type {
		return nil
	}

	return updatePlatformIsExternal(platform)
}

func getUpdateParamsForPlatformBM(platform *models.Platform, userManagedNetworking *bool, cluster *common.Cluster) (*models.Platform, *bool, error) {
	if !swag.BoolValue(userManagedNetworking) && (platform == nil || isPlatformBM(platform)) {
		// Platform is already baremetal, nothing to do
		return nil, nil, nil
	}

	if (platform != nil && isPlatformNone(platform)) || (swag.BoolValue(userManagedNetworking) && platform == nil) {
		return createPlatformFromType(models.PlatformTypeNone), swag.Bool(true), nil
	}

	return updatePlatformIsExternal(platform), userManagedNetworking, nil
}

func getUpdateParamsForPlatformUMNMandatory(platform *models.Platform, userManagedNetworking *bool, cluster *common.Cluster) (*models.Platform, *bool, error) {
	if (userManagedNetworking == nil || *userManagedNetworking) && isUMNMandatoryForPlatform(platform) {
		// userManagedNetworking is already set to true, nothing to do
		return checkPlaformUpdate(platform, cluster), nil, nil
	}

	if *cluster.HighAvailabilityMode == models.ClusterHighAvailabilityModeNone {
		if !swag.BoolValue(userManagedNetworking) || (platform != nil && !isUMNMandatoryForPlatform(platform)) {
			return nil, nil, common.NewApiError(http.StatusBadRequest, errors.New("disabling User Managed Networking or setting platform different than none or oci platforms is not allowed in single node Openshift"))
		}
	}

	if !swag.BoolValue(userManagedNetworking) {
		if platform == nil || isPlatformBM(platform) {
			if cluster.CPUArchitecture != common.X86CPUArchitecture &&
				!featuresupport.IsFeatureAvailable(models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING, cluster.OpenshiftVersion, swag.String(cluster.CPUArchitecture)) {
				return nil, nil, common.NewApiError(http.StatusBadRequest, errors.New("disabling User Managed Networking or setting Bare-Metal platform is not allowed for clusters with non-x86_64 CPU architecture"))
			}

			return createPlatformFromType(models.PlatformTypeBaremetal), swag.Bool(false), nil
		}
	}

	return updatePlatformIsExternal(platform), userManagedNetworking, nil
}

func GetActualUpdateClusterPlatformParams(platform *models.Platform, userManagedNetworking *bool, cluster *common.Cluster) (*models.Platform, *bool, error) {
	if platform == nil && userManagedNetworking == nil {
		return nil, nil, nil
	}

	if err := checkPlatformWrongParamsInput(platform, userManagedNetworking, cluster); err != nil {
		return nil, nil, err
	}

	setExternalDefaultValues(platform, cluster)

	if doesPlatformAllowUMNOrCMN(platform, cluster) {
		return updatePlatformIsExternal(platform), userManagedNetworking, nil
	}

	if isClusterPlatformBM(cluster) {
		return getUpdateParamsForPlatformBM(platform, userManagedNetworking, cluster)
	} else if isUMNMandatoryForCluster(cluster) {
		return getUpdateParamsForPlatformUMNMandatory(platform, userManagedNetworking, cluster)
	} else if platform == nil && !isUMNAllowedForCluster(cluster) && swag.BoolValue(userManagedNetworking) {
		return nil, nil, common.NewApiError(http.StatusBadRequest, errors.Errorf("User-managed-networking is not supported with platform %s", common.PlatformTypeValue(cluster.Platform.Type)))
	}

	return updatePlatformIsExternal(platform), userManagedNetworking, nil
}

func GetClusterPlatformByHighAvailabilityMode(platform *models.Platform, userManagedNetworking *bool, highAvailabilityMode *string) (*models.Platform, *bool, error) {
	if swag.StringValue(highAvailabilityMode) == models.ClusterHighAvailabilityModeFull {
		if (platform == nil || isPlatformBM(platform)) && !swag.BoolValue(userManagedNetworking) {
			return createPlatformFromType(models.PlatformTypeBaremetal), swag.Bool(false), nil
		}

		if swag.BoolValue(userManagedNetworking) || platform == nil || isUMNMandatoryForPlatform(platform) {
			if platform == nil {
				// default to None platform
				return createPlatformFromType(models.PlatformTypeNone), swag.Bool(true), nil
			} else {
				return updatePlatformIsExternal(platform), swag.Bool(true), nil
			}
		}
	} else { // *highAvailabilityMode == models.ClusterHighAvailabilityModeNone
		if isPlatformBM(platform) {
			return nil, nil, common.NewApiError(http.StatusBadRequest, errors.Errorf("Can't set %s platform on single node OpenShift", *platform.Type))
		}

		if userManagedNetworking != nil && !*userManagedNetworking {
			return nil, nil, common.NewApiError(http.StatusBadRequest, errors.New("Can't disable user-managed-networking on single node OpenShift"))
		}

		if isPlatformNone(platform) || common.IsPlatformExternal(platform) {
			return updatePlatformIsExternal(platform), swag.Bool(true), nil
		}

		return createPlatformFromType(models.PlatformTypeNone), swag.Bool(true), nil
	}
	return nil, nil, common.NewApiError(http.StatusBadRequest, errors.Errorf("Got invalid platform (%s) and/or user-managed-networking (%v)", *platform.Type, userManagedNetworking))
}

func GetActualCreateClusterPlatformParams(platform *models.Platform, userManagedNetworking *bool, highAvailabilityMode *string, cpuArchitecture string) (*models.Platform, *bool, error) {
	if err := checkPlatformWrongParamsInput(platform, userManagedNetworking, nil); err != nil {
		return nil, nil, err
	}

	setExternalDefaultValues(platform, nil)

	if cpuArchitecture == models.ClusterCPUArchitectureS390x || cpuArchitecture == models.ClusterCPUArchitecturePpc64le {
		if userManagedNetworking != nil && !*userManagedNetworking {
			return nil, nil, common.NewApiError(http.StatusBadRequest, errors.Errorf("Can't disable User Managed Networking on %s architecture", cpuArchitecture))
		} else if platform != nil && !isPlatformNone(platform) {
			return nil, nil, common.NewApiError(http.StatusBadRequest, errors.Errorf("Can't set %s platform on %s architecture", *platform.Type, cpuArchitecture))
		}
		return createPlatformFromType(models.PlatformTypeNone), swag.Bool(true), nil
	}

	if platform != nil && !isPlatformBM(platform) && !isUMNMandatoryForPlatform(platform) {
		return updatePlatformIsExternal(platform), userManagedNetworking, nil
	}
	return GetClusterPlatformByHighAvailabilityMode(platform, userManagedNetworking, highAvailabilityMode)
}

func GetPlatformFeatureID(platformType models.PlatformType) models.FeatureSupportLevelID {
	switch platformType {
	case models.PlatformTypeExternal:
		return models.FeatureSupportLevelIDEXTERNALPLATFORM
	case models.PlatformTypeVsphere:
		return models.FeatureSupportLevelIDVSPHEREINTEGRATION
	case models.PlatformTypeNutanix:
		return models.FeatureSupportLevelIDNUTANIXINTEGRATION
	default:
		return "" // Return empty string on platform without a feature support ID
	}
}
