package featuresupport

import (
	"fmt"
	"reflect"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

func GetSupportLevel[T models.FeatureSupportLevelID | models.ArchitectureSupportLevelID](featureId T, filters interface{}) models.SupportLevel {
	if reflect.TypeOf(featureId).Name() == "FeatureSupportLevelID" {
		return featuresList[models.FeatureSupportLevelID(featureId)].getSupportLevel(filters.(SupportLevelFilters))
	}
	return cpuFeaturesList[models.ArchitectureSupportLevelID(featureId)].getSupportLevel(filters.(string))
}

func ValidateIncompatibleFeatures(log logrus.FieldLogger, cpuArchitecture string, cluster *common.Cluster, infraEnv *models.InfraEnv, updateParams interface{}) error {
	var openshiftVersion *string
	if cluster != nil {
		openshiftVersion = &cluster.OpenshiftVersion
	}

	activatedFeatures := getActivatedFeatures(log, cluster, infraEnv, updateParams)
	if cpuArchitecture != "" && swag.StringValue(openshiftVersion) != "" {
		if isSupported := isArchitectureSupported(cpuArchitectureFeatureIdMap[cpuArchitecture], swag.StringValue(openshiftVersion)); !isSupported {
			return fmt.Errorf("cannot use %s architecture because it's not compatible on version %s of OpenShift", cpuArchitecture, cluster.OpenshiftVersion)
		}

		if err := isFeaturesCompatible(swag.StringValue(openshiftVersion), cpuArchitecture, activatedFeatures); err != nil {
			return err
		}

	} else {
		log.Warn("Cannot validate incompatible CPU architecture due to empty CPU architecture or empty OpenshiftVersion")
	}

	if err := isFeaturesCompatibleWithFeatures(swag.StringValue(openshiftVersion), activatedFeatures); err != nil {
		return err
	}
	return nil
}

func getActivatedFeatures(log logrus.FieldLogger, cluster *common.Cluster, infraEnv *models.InfraEnv, updateParams interface{}) []SupportLevelFeature {
	var activatedFeatures []SupportLevelFeature
	var clusterUpdateParams *models.V2ClusterUpdateParams
	var infraenvUpdateParams *models.InfraEnvUpdateParams

	if updateParams != nil {
		t := reflect.Indirect(reflect.ValueOf(updateParams))
		if t.Type().Name() == "V2ClusterUpdateParams" {
			clusterUpdateParams = updateParams.(*models.V2ClusterUpdateParams)
		} else if t.Type().Name() == "InfraEnvUpdateParams" {
			infraenvUpdateParams = updateParams.(*models.InfraEnvUpdateParams)
		} else {
			panic("updateParams must be one of type *models.V2ClusterUpdateParams or *models.InfraEnvUpdateParams")
		}
	}

	for _, feature := range featuresList {
		if feature.getFeatureActiveLevel(cluster, infraEnv, clusterUpdateParams, infraenvUpdateParams) == activeLevelActive {
			activatedFeatures = append(activatedFeatures, feature)
			log.Debugf("%s feature is activated", feature.getName())
		}
	}

	return activatedFeatures
}

func IsFeatureCompatibleWithArchitecture(feature models.FeatureSupportLevelID, openshiftVersion, cpuArchitecture string) bool {
	return isFeatureCompatibleWithArchitecture(featuresList[feature], openshiftVersion, cpuArchitecture)
}

func isFeatureCompatibleWithArchitecture(feature SupportLevelFeature, openshiftVersion, cpuArchitecture string) bool {
	architectureID := cpuArchitectureFeatureIdMap[cpuArchitecture]
	incompatibilitiesArchitectures := feature.getIncompatibleArchitectures(&openshiftVersion)
	if incompatibilitiesArchitectures != nil && funk.Contains(*incompatibilitiesArchitectures, architectureID) {
		return false
	}
	return true
}

// isPlatformActive return true if the cluster Platform is set with the given platform or not
// This method take into consideration the update params and the different combination of those arguments
func isPlatformActive(cluster *common.Cluster, clusterUpdateParams *models.V2ClusterUpdateParams, expectedPlatform models.PlatformType) bool {
	if cluster == nil {
		return false
	}

	if (cluster.Platform != nil && common.PlatformTypeValue(cluster.Platform.Type) == expectedPlatform && clusterUpdateParams == nil) ||
		(cluster.Platform != nil && common.PlatformTypeValue(cluster.Platform.Type) == expectedPlatform && clusterUpdateParams != nil && (clusterUpdateParams.Platform == nil || common.PlatformTypeValue(clusterUpdateParams.Platform.Type) == expectedPlatform)) ||
		((cluster.Platform != nil && common.PlatformTypeValue(cluster.Platform.Type) != expectedPlatform) && clusterUpdateParams != nil && (clusterUpdateParams.Platform != nil && common.PlatformTypeValue(clusterUpdateParams.Platform.Type) == expectedPlatform)) {
		return true
	}
	return false
}
