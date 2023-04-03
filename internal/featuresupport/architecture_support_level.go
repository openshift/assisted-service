package featuresupport

import (
	"github.com/openshift/assisted-service/models"
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

func getArchitectureSupportList(features map[models.ArchitectureSupportLevelID]SupportLevelArchitecture, openshiftVersion string) models.SupportLevels {
	featureSupportList := models.SupportLevels{}

	for _, feature := range features {
		featureID := feature.getId()
		featureSupportList[string(featureID)] = feature.getSupportLevel(openshiftVersion)
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
			supportLevels[string(feature.getId())] = models.SupportLevelUnavailable
		}
		return supportLevels
	}
	return nil
}

func GetCpuArchitectureSupportList(openshiftVersion string) models.SupportLevels {
	return getArchitectureSupportList(cpuFeaturesList, openshiftVersion)
}

func isArchitectureSupported(featureId models.ArchitectureSupportLevelID, openshiftVersion string) bool {
	supportLevel := GetSupportLevel(featureId, openshiftVersion)
	return supportLevel != models.SupportLevelUnsupported && supportLevel != models.SupportLevelUnavailable
}
