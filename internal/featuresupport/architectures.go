package featuresupport

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

type SupportLevelArchitecture interface {
	GetId() models.ArchitectureSupportLevelID
	GetSupportLevel(openshiftVersion string) models.SupportLevel
}

// Arm64ArchitectureFeature
type Arm64ArchitectureFeature struct{}

func (feature *Arm64ArchitectureFeature) GetId() models.ArchitectureSupportLevelID {
	return models.ArchitectureSupportLevelIDARM64ARCHITECTURE
}

func (feature *Arm64ArchitectureFeature) GetSupportLevel(openshiftVersion string) models.SupportLevel {
	isNotSupported, err := common.BaseVersionLessThan("4.10", openshiftVersion)
	if isNotSupported || err != nil {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

// X8664ArchitectureFeature
type X8664ArchitectureFeature struct{}

func (feature *X8664ArchitectureFeature) GetId() models.ArchitectureSupportLevelID {
	return models.ArchitectureSupportLevelIDX8664ARCHITECTURE
}

func (feature *X8664ArchitectureFeature) GetSupportLevel(_ string) models.SupportLevel {
	return models.SupportLevelSupported
}

// S390xArchitectureFeature
type S390xArchitectureFeature struct{}

func (feature *S390xArchitectureFeature) GetId() models.ArchitectureSupportLevelID {
	return models.ArchitectureSupportLevelIDS390XARCHITECTURE
}

func (feature *S390xArchitectureFeature) GetSupportLevel(openshiftVersion string) models.SupportLevel {
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

func (feature *PPC64LEArchitectureFeature) GetId() models.ArchitectureSupportLevelID {
	return models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE
}

func (feature *PPC64LEArchitectureFeature) GetSupportLevel(openshiftVersion string) models.SupportLevel {
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

func (feature *MultiArchReleaseImageFeature) GetId() models.ArchitectureSupportLevelID {
	return models.ArchitectureSupportLevelIDMULTIARCHRELEASEIMAGE
}

func (feature *MultiArchReleaseImageFeature) GetSupportLevel(openshiftVersion string) models.SupportLevel {
	if isNotSupported, err := common.BaseVersionLessThan("4.11", openshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelTechPreview
}
