package featuresupport

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

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

func isExternalIntegrationActive(cluster *common.Cluster, clusterUpdateParams *models.V2ClusterUpdateParams, expectedPlatformName string) bool {
	if cluster == nil {
		return false
	}

	if clusterUpdateParams != nil &&
		clusterUpdateParams.Platform != nil &&
		clusterUpdateParams.Platform.External != nil &&
		clusterUpdateParams.Platform.External.PlatformName != nil &&
		*clusterUpdateParams.Platform.External.PlatformName == expectedPlatformName {
		return true
	}

	if cluster.Platform != nil &&
		cluster.Platform.External != nil &&
		cluster.Platform.External.PlatformName != nil &&
		*cluster.Platform.External.PlatformName == expectedPlatformName {
		return true
	}

	return false
}

func isPlatformSet(filters SupportLevelFilters) bool {
	return filters.PlatformType != nil
}

// BaremetalPlatformFeature
type BaremetalPlatformFeature struct{}

func (feature *BaremetalPlatformFeature) New() SupportLevelFeature {
	return &BaremetalPlatformFeature{}
}

func (feature *BaremetalPlatformFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDBAREMETALPLATFORM
}

func (feature *BaremetalPlatformFeature) GetName() string {
	return "Baremetal Platform Integration"
}

func (feature *BaremetalPlatformFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if isPlatformSet(filters) {
		return ""
	}

	return models.SupportLevelSupported
}

func (feature *BaremetalPlatformFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isPlatformActive(cluster, clusterUpdateParams, models.PlatformTypeBaremetal) {
		return activeLevelActive
	}

	return activeLevelNotActive
}

func (feature *BaremetalPlatformFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDPLATFORMMANAGEDNETWORKING,
		models.FeatureSupportLevelIDUSERMANAGEDNETWORKING,
	}
}

func (feature *BaremetalPlatformFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

// NonePlatformFeature
type NonePlatformFeature struct{}

func (feature *NonePlatformFeature) New() SupportLevelFeature {
	return &NonePlatformFeature{}
}

func (feature *NonePlatformFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNONEPLATFORM
}

func (feature *NonePlatformFeature) GetName() string {
	return "None Platform Integration"
}

func (feature *NonePlatformFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if isPlatformSet(filters) {
		return ""
	}

	return models.SupportLevelSupported
}

func (feature *NonePlatformFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isPlatformActive(cluster, clusterUpdateParams, models.PlatformTypeNone) {
		return activeLevelActive
	}

	return activeLevelNotActive
}

func (feature *NonePlatformFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDVIPAUTOALLOC,
		models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
	}
}

func (feature *NonePlatformFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

// NutanixIntegrationFeature
type NutanixIntegrationFeature struct{}

func (feature *NutanixIntegrationFeature) New() SupportLevelFeature {
	return &NutanixIntegrationFeature{}
}

func (feature *NutanixIntegrationFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNUTANIXINTEGRATION
}

func (feature *NutanixIntegrationFeature) GetName() string {
	return "Nutanix Platform Integration"
}

func (feature *NutanixIntegrationFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if isPlatformSet(filters) {
		return ""
	}

	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	if isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable
	}

	if isEqual, _ := common.BaseVersionEqual("4.11", filters.OpenshiftVersion); isEqual {
		return models.SupportLevelDevPreview
	}
	return models.SupportLevelSupported
}

func (feature *NutanixIntegrationFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isPlatformActive(cluster, clusterUpdateParams, models.PlatformTypeNutanix) {
		return activeLevelActive
	}

	return activeLevelNotActive
}

func (feature *NutanixIntegrationFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDUSERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDLVM,
		models.FeatureSupportLevelIDMCE,
		models.FeatureSupportLevelIDCNV,
		models.FeatureSupportLevelIDPLATFORMMANAGEDNETWORKING,
	}
}

func (feature *NutanixIntegrationFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	}
}

// VsphereIntegrationFeature
type VsphereIntegrationFeature struct{}

func (feature *VsphereIntegrationFeature) New() SupportLevelFeature {
	return &VsphereIntegrationFeature{}
}

func (feature *VsphereIntegrationFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDVSPHEREINTEGRATION
}

func (feature *VsphereIntegrationFeature) GetName() string {
	return "vSphere Platform Integration"
}

func (feature *VsphereIntegrationFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if isPlatformSet(filters) {
		return ""
	}

	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *VsphereIntegrationFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isPlatformActive(cluster, clusterUpdateParams, models.PlatformTypeVsphere) {
		return activeLevelActive
	}

	return activeLevelNotActive
}

func (feature *VsphereIntegrationFeature) getIncompatibleFeatures(openshiftVersion string) *[]models.FeatureSupportLevelID {
	incompatibleFeatures := []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDLVM,
		models.FeatureSupportLevelIDPLATFORMMANAGEDNETWORKING,
		models.FeatureSupportLevelIDCNV,
	}

	if isNotSupported, err := common.BaseVersionLessThan("4.13", openshiftVersion); isNotSupported || err != nil {
		incompatibleFeatures = append(incompatibleFeatures, models.FeatureSupportLevelIDDUALSTACK)
	}

	return &incompatibleFeatures
}

func (feature *VsphereIntegrationFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

// OciIntegrationFeature
type OciIntegrationFeature struct{}

func (feature *OciIntegrationFeature) New() SupportLevelFeature {
	return &OciIntegrationFeature{}
}

func (feature *OciIntegrationFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDEXTERNALPLATFORMOCI
}

func (feature *OciIntegrationFeature) GetName() string {
	return "Oracle Cloud Infrastructure external platform"
}

func (feature *OciIntegrationFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if isPlatformSet(filters) {
		return ""
	}

	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	if isSupported, err := common.BaseVersionGreaterOrEqual("4.14", filters.OpenshiftVersion); isSupported || err != nil {
		return models.SupportLevelSupported
	}

	return models.SupportLevelUnavailable
}

func (feature *OciIntegrationFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDVIPAUTOALLOC,
		models.FeatureSupportLevelIDDUALSTACKVIPS,
		models.FeatureSupportLevelIDFULLISO,
	}
}

func (feature *OciIntegrationFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

func (feature *OciIntegrationFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isPlatformActive(cluster, clusterUpdateParams, models.PlatformTypeExternal) && isExternalIntegrationActive(cluster, clusterUpdateParams, common.ExternalPlatformNameOci) {
		return activeLevelActive
	}

	return activeLevelNotActive
}

// ExternalPlatformFeature
type ExternalPlatformFeature struct{}

func (feature *ExternalPlatformFeature) New() SupportLevelFeature {
	return &ExternalPlatformFeature{}
}

func (feature *ExternalPlatformFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDEXTERNALPLATFORM
}

func (feature *ExternalPlatformFeature) GetName() string {
	return "External Platform Integration"
}

func (feature *ExternalPlatformFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if isPlatformSet(filters) {
		return ""
	}

	if isNotSupported, err := common.BaseVersionLessThan("4.14", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *ExternalPlatformFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDVIPAUTOALLOC,
	}
}

func (feature *ExternalPlatformFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *ExternalPlatformFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isPlatformActive(cluster, clusterUpdateParams, models.PlatformTypeExternal) {
		return activeLevelActive
	}

	return activeLevelNotActive
}
