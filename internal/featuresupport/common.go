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
		return featuresList[models.FeatureSupportLevelID(featureId)].getSupportLevel(filters.(SupportLevelFilters), true)
	}
	return cpuFeaturesList[models.ArchitectureSupportLevelID(featureId)].getSupportLevel(filters.(string))
}

func ValidateIncompatibleFeatures(log logrus.FieldLogger, cpuArchitecture string, cluster *common.Cluster, infraEnv *models.InfraEnv, updateParams interface{}) error {
	var openshiftVersion *string
	var highAvailabilityMode *string
	if cluster != nil {
		openshiftVersion = &cluster.OpenshiftVersion
		highAvailabilityMode = cluster.HighAvailabilityMode
	}

	activatedFeatures := getActivatedFeatures(log, cluster, infraEnv, updateParams)
	if cpuArchitecture != "" && swag.StringValue(openshiftVersion) != "" {
		filters := SupportLevelFilters{OpenshiftVersion: *openshiftVersion, CPUArchitecture: &cpuArchitecture}
		if isSupported := isArchitectureSupported(filters); !isSupported {
			return fmt.Errorf("cannot use %s architecture because it's not compatible on version %s of OpenShift", cpuArchitecture, cluster.OpenshiftVersion)
		}

		if err := isFeaturesCompatible(swag.StringValue(openshiftVersion), cpuArchitecture, highAvailabilityMode, activatedFeatures); err != nil {
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
			log.Debugf("%s feature is activated", feature.GetName())
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
