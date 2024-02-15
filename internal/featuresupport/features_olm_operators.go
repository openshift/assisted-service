package featuresupport

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
)

func getOperatorsList(cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) (*[]string, *[]string) {
	var clusterOperators []string
	var updateParamsOperators []string

	if updateParams != nil && updateParams.OlmOperators != nil {
		for _, operatorParams := range updateParams.OlmOperators {
			updateParamsOperators = append(updateParamsOperators, operatorParams.Name)
		}
	}

	if cluster.MonitoredOperators != nil {
		for _, operatorParams := range cluster.MonitoredOperators {
			clusterOperators = append(clusterOperators, operatorParams.Name)
		}
	}

	return &clusterOperators, &updateParamsOperators
}

func isOperatorActivated(operator string, cluster *common.Cluster, updateParams *models.V2ClusterUpdateParams) bool {
	if cluster == nil {
		return false
	}
	activeOperators, updatedOperators := getOperatorsList(*cluster, updateParams)
	operatorActivated := activeOperators != nil && (funk.Contains(*activeOperators, operator))
	operatorUpdated := updatedOperators != nil && (funk.Contains(*updatedOperators, operator))

	return (operatorActivated && (updateParams == nil || updateParams.OlmOperators == nil)) || operatorActivated && operatorUpdated || operatorUpdated
}

// LvmFeature
type LvmFeature struct{}

func (feature *LvmFeature) New() SupportLevelFeature {
	return &LvmFeature{}
}

func (feature *LvmFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDLVM
}

func (feature *LvmFeature) GetName() string {
	return "Logical Volume Management"
}

func (feature *LvmFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	if isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable
	}

	if filters.PlatformType != nil && (*filters.PlatformType == models.PlatformTypeVsphere || *filters.PlatformType == models.PlatformTypeNutanix) {
		return models.SupportLevelUnavailable
	}

	if isEqual, _ := common.BaseVersionEqual("4.11", filters.OpenshiftVersion); isEqual {
		return models.SupportLevelDevPreview
	}

	return models.SupportLevelSupported
}

func (feature *LvmFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("lvm", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *LvmFeature) getIncompatibleFeatures(OCPVersion string) *[]models.FeatureSupportLevelID {
	incompatibleFeatures := []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
		models.FeatureSupportLevelIDODF,
	}
	if isEqual, _ := common.BaseVersionLessThan("4.15", OCPVersion); isEqual {
		incompatibleFeatures = append(incompatibleFeatures,
			models.FeatureSupportLevelIDVIPAUTOALLOC,
			models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
		)
	}
	return &incompatibleFeatures
}

func (feature *LvmFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

// OdfFeature
type OdfFeature struct{}

func (feature *OdfFeature) New() SupportLevelFeature {
	return &OdfFeature{}
}

func (feature *OdfFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDODF
}

func (feature *OdfFeature) GetName() string {
	return "OpenShift Data Foundation"
}

func (feature *OdfFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *OdfFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	}
}

func (feature *OdfFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDLVM,
	}
}

func (feature *OdfFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("odf", cluster, clusterUpdateParams) || isOperatorActivated("ocs", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// CnvFeature
type CnvFeature struct{}

func (feature *CnvFeature) New() SupportLevelFeature {
	return &CnvFeature{}
}

func (feature *CnvFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDCNV
}

func (feature *CnvFeature) GetName() string {
	return "OpenShift Virtualization"
}

func (feature *CnvFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	if filters.PlatformType != nil && (*filters.PlatformType == models.PlatformTypeNutanix || *filters.PlatformType == models.PlatformTypeVsphere) {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *CnvFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
	}
}

func (feature *CnvFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	}
}

func (feature *CnvFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("cnv", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// LsoFeature
type LsoFeature struct{}

func (feature *LsoFeature) New() SupportLevelFeature {
	return &LsoFeature{}
}

func (feature *LsoFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDLSO
}

func (feature *LsoFeature) GetName() string {
	return "Local Storage Operator"
}

func (feature *LsoFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *LsoFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *LsoFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	}
}

func (feature *LsoFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("lso", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// MceFeature
type MceFeature struct{}

func (feature *MceFeature) New() SupportLevelFeature {
	return &MceFeature{}
}

func (feature *MceFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDMCE
}

func (feature *MceFeature) GetName() string {
	return "multicluster engine"
}

func (feature *MceFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	isNotSupported, err := common.BaseVersionLessThan("4.10", filters.OpenshiftVersion)
	if isNotSupported || err != nil {
		return models.SupportLevelUnavailable
	}

	if filters.PlatformType != nil && (*filters.PlatformType == models.PlatformTypeNutanix) {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *MceFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
	}
}

func (feature *MceFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *MceFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("mce", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}
