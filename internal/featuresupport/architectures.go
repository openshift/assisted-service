package featuresupport

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

type SupportLevelArchitecture interface {
	getId() models.ArchitectureSupportLevelID
	getSupportLevel(openshiftVersion string) models.SupportLevel
}

// Arm64ArchitectureFeature
type Arm64ArchitectureFeature struct{}

func (feature *Arm64ArchitectureFeature) getId() models.ArchitectureSupportLevelID {
	return models.ArchitectureSupportLevelIDARM64ARCHITECTURE
}

func (feature *Arm64ArchitectureFeature) getSupportLevel(openshiftVersion string) models.SupportLevel {
	isNotSupported, err := common.BaseVersionLessThan("4.10", openshiftVersion)
	if isNotSupported || err != nil {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

// X8664ArchitectureFeature
type X8664ArchitectureFeature struct{}

func (feature *X8664ArchitectureFeature) getId() models.ArchitectureSupportLevelID {
	return models.ArchitectureSupportLevelIDX8664ARCHITECTURE
}

func (feature *X8664ArchitectureFeature) getSupportLevel(_ string) models.SupportLevel {
	return models.SupportLevelSupported
}

// S390xArchitectureFeature
type S390xArchitectureFeature struct{}

func (feature *S390xArchitectureFeature) getId() models.ArchitectureSupportLevelID {
	return models.ArchitectureSupportLevelIDS390XARCHITECTURE
}

func (feature *S390xArchitectureFeature) getSupportLevel(openshiftVersion string) models.SupportLevel {
	if isNotAvailable, err := common.BaseVersionLessThan("4.12", openshiftVersion); isNotAvailable || err != nil {
		return models.SupportLevelUnavailable
	}
	if isEqual, _ := common.BaseVersionEqual("4.12", openshiftVersion); isEqual {
		return models.SupportLevelTechPreview
	}

	return models.SupportLevelSupported
}

// PPC64LEArchitectureFeature
type PPC64LEArchitectureFeature struct{}

func (feature *PPC64LEArchitectureFeature) getId() models.ArchitectureSupportLevelID {
	return models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE
}

func (feature *PPC64LEArchitectureFeature) getSupportLevel(openshiftVersion string) models.SupportLevel {
	if isNotAvailable, err := common.BaseVersionLessThan("4.12", openshiftVersion); isNotAvailable || err != nil {
		return models.SupportLevelUnavailable
	}
	if isEqual, _ := common.BaseVersionEqual("4.12", openshiftVersion); isEqual {
		return models.SupportLevelTechPreview
	}

	return models.SupportLevelSupported
}

// MultiArchReleaseImageFeature
type MultiArchReleaseImageFeature struct{}

func (feature *MultiArchReleaseImageFeature) getId() models.ArchitectureSupportLevelID {
	return models.ArchitectureSupportLevelIDMULTIARCHRELEASEIMAGE
}

func (feature *MultiArchReleaseImageFeature) getSupportLevel(openshiftVersion string) models.SupportLevel {
	if isNotSupported, err := common.BaseVersionLessThan("4.11", openshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelTechPreview
}
