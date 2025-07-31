package featuresupport

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

// SnoFeature
type SnoFeature struct{}

func (feature *SnoFeature) New() SupportLevelFeature {
	return &SnoFeature{}
}

func (feature *SnoFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSNO
}

func (feature *SnoFeature) GetName() string {
	return "Single Node OpenShift"
}

func (feature *SnoFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	// Sno is not available with Nutanix / Vsphere platforms
	if filters.PlatformType != nil && (*filters.PlatformType == models.PlatformTypeNutanix || *filters.PlatformType == models.PlatformTypeVsphere) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonPlatform
	}

	if swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureS390x || swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitecturePpc64le {
		if isEqual, _ := common.BaseVersionEqual("4.13", filters.OpenshiftVersion); isEqual {
			return models.SupportLevelDevPreview, ""
		}
	}

	return models.SupportLevelSupported, ""
}

func (feature *SnoFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDODF,
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
		models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDVIPAUTOALLOC,
		models.FeatureSupportLevelIDOPENSHIFTAI,
		models.FeatureSupportLevelIDUSERMANAGEDLOADBALANCER,
		models.FeatureSupportLevelIDNODEHEALTHCHECK,
		models.FeatureSupportLevelIDSELFNODEREMEDIATION,
		models.FeatureSupportLevelIDFENCEAGENTSREMEDIATION,
		models.FeatureSupportLevelIDNODEMAINTENANCE,
		models.FeatureSupportLevelIDKUBEDESCHEDULER,
	}
}

func (feature *SnoFeature) getIncompatibleArchitectures(openshiftVersion *string) []models.ArchitectureSupportLevelID {
	if isGreater, _ := common.BaseVersionGreaterOrEqual("4.13", *openshiftVersion); isGreater {
		return nil
	}

	// Not supported when OCP version is less than 4.13
	return []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

func (feature *SnoFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, _ *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if cluster != nil && cluster.ControlPlaneCount == 1 {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// TnaFeature
type TnaFeature struct{}

func (feature *TnaFeature) New() SupportLevelFeature {
	return &TnaFeature{}
}

func (feature *TnaFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDTNA
}

func (feature *TnaFeature) GetName() string {
	return "TNA Clusters"
}

func (feature *TnaFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	//TNA is only available with baremetal platform
	if filters.PlatformType != nil && *filters.PlatformType != models.PlatformTypeBaremetal {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonPlatform
	}

	// If we equal minimum version then we are in TechPreview support
	if arbiterClustersSupported, _ := common.BaseVersionEqual(common.MinimumVersionForArbiterClusters, filters.OpenshiftVersion); arbiterClustersSupported {
		return models.SupportLevelTechPreview, ""
	}

	// If we did not equal minimum and are greater, then we are in normal support level
	if arbiterClustersSupported, _ := common.BaseVersionGreaterOrEqual(common.MinimumVersionForArbiterClusters, filters.OpenshiftVersion); arbiterClustersSupported {
		return models.SupportLevelSupported, ""
	}

	return models.SupportLevelUnavailable, models.IncompatibilityReasonOpenshiftVersion
}

func (feature *TnaFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDNONEPLATFORM,
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
		models.FeatureSupportLevelIDEXTERNALPLATFORM,
		models.FeatureSupportLevelIDEXTERNALPLATFORMOCI,
	}
}

func (feature *TnaFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return nil
}

func (feature *TnaFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, _ *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if common.IsClusterTopologyHighlyAvailableArbiter(cluster) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// CustomManifestFeature
type CustomManifestFeature struct{}

func (feature *CustomManifestFeature) New() SupportLevelFeature {
	return &CustomManifestFeature{}
}

func (feature *CustomManifestFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDCUSTOMMANIFEST
}

func (feature *CustomManifestFeature) GetName() string {
	return "Custom Manifest"
}

func (feature *CustomManifestFeature) getSupportLevel(_ SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	return models.SupportLevelSupported, ""
}

func (feature *CustomManifestFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return nil
}

func (feature *CustomManifestFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return nil
}

func (feature *CustomManifestFeature) getFeatureActiveLevel(_ *common.Cluster, _ *models.InfraEnv, _ *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	return activeLevelNotRelevant
}

// SingleNodeExpansionFeature
type SingleNodeExpansionFeature struct {
	snoFeature SnoFeature
}

func (feature *SingleNodeExpansionFeature) New() SupportLevelFeature {
	return &SingleNodeExpansionFeature{
		SnoFeature{},
	}
}

func (feature *SingleNodeExpansionFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSINGLENODEEXPANSION
}

func (feature *SingleNodeExpansionFeature) GetName() string {
	return "Single Node Expansion"
}

func (feature *SingleNodeExpansionFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion)
	if isNotSupported || err != nil {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonOpenshiftVersion
	}

	return feature.snoFeature.getSupportLevel(filters)
}

func (feature *SingleNodeExpansionFeature) getFeatureActiveLevel(_ *common.Cluster, _ *models.InfraEnv, _ *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	return activeLevelNotRelevant
}

func (feature *SingleNodeExpansionFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return nil
}

func (feature *SingleNodeExpansionFeature) getIncompatibleArchitectures(openshiftVersion *string) []models.ArchitectureSupportLevelID {
	return feature.snoFeature.getIncompatibleArchitectures(openshiftVersion)
}

// MinimalIso
type MinimalIso struct{}

func (feature *MinimalIso) New() SupportLevelFeature {
	return &MinimalIso{}
}

func (feature *MinimalIso) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDMINIMALISO
}

func (feature *MinimalIso) GetName() string {
	return "Minimal ISO"
}

func (feature *MinimalIso) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	return models.SupportLevelSupported, ""
}

func (feature *MinimalIso) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return nil
}

func (feature *MinimalIso) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
	}
}

func (feature *MinimalIso) getFeatureActiveLevel(_ *common.Cluster, infraEnv *models.InfraEnv, _ *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
	if infraEnv == nil || infraEnv.Type == nil {
		return activeLevelNotActive
	}

	if infraenvUpdateParams != nil {
		if string(infraenvUpdateParams.ImageType) == string(models.ImageTypeMinimalIso) {
			return activeLevelActive
		} else if string(infraenvUpdateParams.ImageType) == string(models.ImageTypeFullIso) {
			return activeLevelNotActive
		}
	}

	if string(*infraEnv.Type) == string(models.ImageTypeMinimalIso) {
		return activeLevelActive
	}
	if string(*infraEnv.Type) == string(models.ImageTypeFullIso) {
		return activeLevelNotActive
	}
	return activeLevelNotActive
}

// FullIso
type FullIso struct{}

func (feature *FullIso) New() SupportLevelFeature {
	return &FullIso{}
}

func (feature *FullIso) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDFULLISO
}

func (feature *FullIso) GetName() string {
	return "Full ISO"
}

func (feature *FullIso) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	platform := &models.Platform{
		Type: filters.PlatformType,
		External: &models.PlatformExternal{
			PlatformName: filters.ExternalPlatformName,
		},
	}
	if common.IsOciExternalIntegrationEnabled(platform) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonOciExternalIntegrationDisabled
	}

	return models.SupportLevelSupported, ""
}

func (feature *FullIso) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDEXTERNALPLATFORMOCI,
	}
}

func (feature *FullIso) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return nil
}

func (feature *FullIso) getFeatureActiveLevel(_ *common.Cluster, infraEnv *models.InfraEnv, _ *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
	if infraEnv == nil || infraEnv.Type == nil {
		return activeLevelNotActive
	}

	if infraenvUpdateParams != nil {
		if string(infraenvUpdateParams.ImageType) == string(models.ImageTypeFullIso) {
			return activeLevelActive
		} else if string(infraenvUpdateParams.ImageType) == string(models.ImageTypeMinimalIso) {
			return activeLevelNotActive
		}
	}

	if string(*infraEnv.Type) == string(models.ImageTypeFullIso) {
		return activeLevelActive
	}
	if string(*infraEnv.Type) == string(models.ImageTypeMinimalIso) {
		return activeLevelNotActive
	}
	return activeLevelNotActive
}

// Non-standard HA OCP Control Plane
type NonStandardHAControlPlane struct{}

func (f *NonStandardHAControlPlane) New() SupportLevelFeature {
	return &NonStandardHAControlPlane{}
}

func (f *NonStandardHAControlPlane) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNONSTANDARDHACONTROLPLANE
}

func (f *NonStandardHAControlPlane) GetName() string {
	return "Non-standard HA OCP Control Plane"
}

func (f *NonStandardHAControlPlane) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	supported, err := common.BaseVersionGreaterOrEqual(common.MinimumVersionForNonStandardHAOCPControlPlane, filters.OpenshiftVersion)
	if !supported || err != nil {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonOpenshiftVersion
	}

	if filters.PlatformType != nil &&
		(*filters.PlatformType != models.PlatformTypeBaremetal && *filters.PlatformType != models.PlatformTypeNone) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonPlatform
	}

	return models.SupportLevelSupported, ""
}

func (f *NonStandardHAControlPlane) getIncompatibleFeatures(openshiftVersion string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDEXTERNALPLATFORM,
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
		models.FeatureSupportLevelIDEXTERNALPLATFORMOCI,
	}
}

func (f *NonStandardHAControlPlane) getIncompatibleArchitectures(openshiftVersion *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
		models.ArchitectureSupportLevelIDMULTIARCHRELEASEIMAGE,
	}
}

func (f *NonStandardHAControlPlane) getFeatureActiveLevel(cluster *common.Cluster, infraEnv *models.InfraEnv,
	clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
	if cluster != nil && cluster.ControlPlaneCount > 3 {
		return activeLevelActive
	}

	return activeLevelNotActive
}
