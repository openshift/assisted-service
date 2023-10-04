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
	return platform != nil && *platform.Type == models.PlatformTypeBaremetal
}

func isPlatformNone(platform *models.Platform) bool {
	return platform != nil && *platform.Type == models.PlatformTypeNone
}

func isClusterPlatformNone(cluster *common.Cluster) bool {
	return cluster != nil && isPlatformNone(cluster.Platform)
}

func isPlatformExternal(platform *models.Platform) bool {
	return platform != nil && *platform.Type == models.PlatformTypeOci
}

func isUMNAllowedForPlatform(platform *models.Platform) bool {
	if platform == nil {
		return true
	}

	switch *platform.Type {
	case models.PlatformTypeBaremetal, models.PlatformTypeNutanix:
		return false
	}
	return true
}

func isUMNMandatoryForPlatform(platform *models.Platform) bool {
	if platform == nil {
		return true
	}

	if *platform.Type == models.PlatformTypeNone || isPlatformExternal(platform) {
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
	platform.IsExternal = swag.Bool(isPlatformExternal(platform))

	return platform
}

func GetActualUpdateClusterPlatformParamsNew(cluster *common.Cluster, clusterUpdateParams *models.V2ClusterUpdateParams) (*models.Platform, error) {
	if clusterUpdateParams == nil && clusterUpdateParams.Platform == nil {
		// nothing to update
		return nil, nil
	}

	// Ignore if cluster platform is the same as the updated platform
	if clusterUpdateParams != nil && clusterUpdateParams.Platform != nil && *clusterUpdateParams.Platform.Type == *cluster.Platform.Type {
		return nil, nil
	}

	// clear VIPs if platform is set
	if featuresupport.IsActivePlatformSupportsUmn(cluster, clusterUpdateParams) {
		cluster.APIVip = ""
		cluster.APIVips = []*models.APIVip{}
		cluster.IngressVip = ""
		cluster.IngressVips = []*models.IngressVip{}
	}

	return updatePlatformIsExternal(clusterUpdateParams.Platform), nil
}

// GetClusterPlatformByHighAvailabilityMode For KUBE-API - TODO will be removed in the future
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

		if isPlatformNone(platform) || isPlatformExternal(platform) {
			return updatePlatformIsExternal(platform), swag.Bool(true), nil
		}

		return createPlatformFromType(models.PlatformTypeNone), swag.Bool(true), nil
	}
	return nil, nil, common.NewApiError(http.StatusBadRequest, errors.Errorf("Got invalid platform (%s) and/or user-managed-networking (%v)", *platform.Type, userManagedNetworking))
}

func GetCreateClusterPlatformParams(platform *models.Platform, highAvailabilityMode *string, cpuArchitecture string) (*models.Platform, error) {
	if platform != nil {
		return updatePlatformIsExternal(platform), nil
	}

	if cpuArchitecture == models.ClusterCPUArchitectureS390x || cpuArchitecture == models.ClusterCPUArchitecturePpc64le {
		return createPlatformFromType(models.PlatformTypeNone), nil
	}

	if swag.StringValue(highAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		if isPlatformNone(platform) || isPlatformExternal(platform) {
			return updatePlatformIsExternal(platform), nil
		}
		return createPlatformFromType(models.PlatformTypeNone), nil
	}

	return createPlatformFromType(models.PlatformTypeBaremetal), nil
}

// GetActualCreateClusterPlatformParams For KUBE-API - TODO will be removed in the future
func GetActualCreateClusterPlatformParams(platform *models.Platform, userManagedNetworking *bool, highAvailabilityMode *string, cpuArchitecture string) (*models.Platform, *bool, error) {
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
	case models.PlatformTypeOci:
		return models.FeatureSupportLevelIDEXTERNALPLATFORMOCI
	case models.PlatformTypeVsphere:
		return models.FeatureSupportLevelIDVSPHEREINTEGRATION
	case models.PlatformTypeNutanix:
		return models.FeatureSupportLevelIDNUTANIXINTEGRATION
	default:
		return "" // Return empty string on platform without a feature support ID
	}
}
