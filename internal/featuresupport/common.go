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

func ValidateActiveFeatures(log logrus.FieldLogger, cluster *common.Cluster, infraEnv *models.InfraEnv, updateParams interface{}) error {
	var err error
	if cluster == nil {
		return err
	}
	activatedFeatures := getActivatedFeatures(log, cluster, infraEnv, updateParams)
	for _, feature := range activatedFeatures {
		logFields := logrus.Fields{
			"cluster_id":            cluster.ID,
			"cluster_version":       swag.String(cluster.OpenshiftVersion),
			"arch":                  swag.String(cluster.CPUArchitecture),
			"support_level_feature": feature.GetName(),
		}
		// Once all features can validate themselves, we can merge SupportLevelFeatureValidator interface
		// with SupportLevelFeature interface and remove this type check
		if validatingFeature, ok := feature.(SupportLevelFeatureValidator); ok {
			log.WithFields(logFields).Infof("validating feature")
			if err := validatingFeature.Validate(cluster, updateParams); err != nil {
				log.WithFields(logFields).WithError(err).Warning("could not validate fature")
				return err
			}
			continue
		}
		log.WithFields(logFields).Info("feature does not implement SupportLevelFeatureValidator, skipping validation")
	}
	return nil
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

	// reflection used to protect against typed nils
	if updateParams != nil && !reflect.ValueOf(updateParams).IsNil() {
		if clusterParams, ok := updateParams.(*models.V2ClusterUpdateParams); ok {
			clusterUpdateParams = clusterParams
		}
		if infraenvParams, ok := updateParams.(*models.InfraEnvUpdateParams); ok {
			infraenvUpdateParams = infraenvParams
		}
		if infraenvUpdateParams == nil && clusterUpdateParams == nil {
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
