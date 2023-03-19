package featuresupport

import (
	"fmt"

	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
)

var cpuFeaturesList = map[models.ArchitectureSupportLevelID]SupportLevelArchitecture{
	models.ArchitectureSupportLevelIDX8664ARCHITECTURE:     &X8664ArchitectureFeature{},
	models.ArchitectureSupportLevelIDARM64ARCHITECTURE:     &Arm64ArchitectureFeature{},
	models.ArchitectureSupportLevelIDS390XARCHITECTURE:     &S390xArchitectureFeature{},
	models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE:   &PPC64LEArchitectureFeature{},
	models.ArchitectureSupportLevelIDMULTIARCHRELEASEIMAGE: &MultiArchReleaseImageFeature{},
}

var cpuArchitectureFeatureIdMap = map[string]models.ArchitectureSupportLevelID{
	models.ClusterCPUArchitectureX8664:   models.ArchitectureSupportLevelIDX8664ARCHITECTURE,
	models.ClusterCPUArchitectureArm64:   models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	models.ClusterCPUArchitectureS390x:   models.ArchitectureSupportLevelIDS390XARCHITECTURE,
	models.ClusterCPUArchitecturePpc64le: models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	models.ClusterCPUArchitectureMulti:   models.ArchitectureSupportLevelIDMULTIARCHRELEASEIMAGE,
}

func isFeatureCompatibleWithArchitecture(feature SupportLevelFeature, openshiftVersion, cpuArchitecture string) bool {
	architectureID := cpuArchitectureFeatureIdMap[cpuArchitecture]
	incompatibilitiesArchitectures := feature.GetIncompatibleArchitectures(openshiftVersion)
	if incompatibilitiesArchitectures != nil && funk.Contains(*incompatibilitiesArchitectures, architectureID) {
		return false
	}
	return true
}

func getArchitectureSupportList(features map[models.ArchitectureSupportLevelID]SupportLevelArchitecture, openshiftVersion string) models.SupportLevels {
	featureSupportList := models.SupportLevels{}

	for _, feature := range features {
		featureID := feature.GetId()
		featureSupportList[string(featureID)] = feature.GetSupportLevel(openshiftVersion)
	}
	return featureSupportList
}

// Handle cases where a CPU architecture is not supported at for a given openshift version, in that case
// return a list of unsupported features
func overrideInvalidRequest(features map[models.FeatureSupportLevelID]SupportLevelFeature, cpuArchitecture, openshiftVersion string) models.SupportLevels {
	supportLevels := models.SupportLevels{}
	cpuArchID := cpuArchitectureFeatureIdMap[cpuArchitecture]
	if !isArchitectureSupported(cpuArchID, openshiftVersion) {
		for _, feature := range features {
			supportLevels[string(feature.GetId())] = models.SupportLevelUnsupported
		}
		return supportLevels
	}
	return nil
}

func GetCpuArchitectureSupportList(openshiftVersion string) models.SupportLevels {
	return getArchitectureSupportList(cpuFeaturesList, openshiftVersion)
}

func isArchitectureSupported(featureId models.ArchitectureSupportLevelID, openshiftVersion string) bool {
	return GetSupportLevel(featureId, openshiftVersion) != models.SupportLevelUnsupported
}

// isFeaturesCompatibleWIthArchitecture Determine if feature is compatible with CPU architecture in a given openshift-version
func isFeaturesCompatibleWIthArchitecture(openshiftVersion, cpuArchitecture string, activatedFeatures []SupportLevelFeature) error {
	for _, feature := range activatedFeatures {
		if !isFeatureCompatibleWithArchitecture(feature, openshiftVersion, cpuArchitecture) {
			return fmt.Errorf("cannot use %s because it's not compatible with the %s architecture "+
				"on version %s of OpenShift", feature.GetName(), cpuArchitecture, openshiftVersion)
		}
	}
	return nil
}
