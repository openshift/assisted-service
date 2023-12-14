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

func (feature *SnoFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	// Sno is not available with Nutanix / Vsphere platforms
	if filters.PlatformType != nil && (*filters.PlatformType == models.PlatformTypeNutanix || *filters.PlatformType == models.PlatformTypeVsphere) {
		return models.SupportLevelUnavailable
	}

	if swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureS390x || swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitecturePpc64le {
		if isEqual, _ := common.BaseVersionEqual("4.13", filters.OpenshiftVersion); isEqual {
			return models.SupportLevelDevPreview
		}
	}

	return models.SupportLevelSupported
}

func (feature *SnoFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDODF,
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
		models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDVIPAUTOALLOC,
	}
}

func (feature *SnoFeature) getIncompatibleArchitectures(openshiftVersion *string) *[]models.ArchitectureSupportLevelID {
	if isGreater, _ := common.BaseVersionGreaterOrEqual("4.13", *openshiftVersion); isGreater {
		return nil
	}

	// Not supported when OCP version is less than 4.13
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

func (feature *SnoFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, _ *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if cluster != nil && swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
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

func (feature *CustomManifestFeature) getSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

func (feature *CustomManifestFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *CustomManifestFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *CustomManifestFeature) getFeatureActiveLevel(_ *common.Cluster, _ *models.InfraEnv, _ *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	return activeLeveNotRelevant
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

func (feature *SingleNodeExpansionFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion)
	if isNotSupported || err != nil {
		return models.SupportLevelUnavailable
	}

	return feature.snoFeature.getSupportLevel(filters)
}

func (feature *SingleNodeExpansionFeature) getFeatureActiveLevel(_ *common.Cluster, _ *models.InfraEnv, _ *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	return activeLeveNotRelevant
}

func (feature *SingleNodeExpansionFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *SingleNodeExpansionFeature) getIncompatibleArchitectures(openshiftVersion *string) *[]models.ArchitectureSupportLevelID {
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

func (feature *MinimalIso) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *MinimalIso) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *MinimalIso) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
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

func (feature *FullIso) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	platform := &models.Platform{
		Type: filters.PlatformType,
		External: &models.PlatformExternal{
			PlatformName: filters.ExternalPlatformName,
		},
	}
	if common.IsOciExternalIntegrationEnabled(platform) {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *FullIso) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDEXTERNALPLATFORMOCI,
	}
}

func (feature *FullIso) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
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

// Skip MCO reboot
type skipMcoReboot struct{}

func (f *skipMcoReboot) New() SupportLevelFeature {
	return &skipMcoReboot{}
}

func (f *skipMcoReboot) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSKIPMCOREBOOT
}

func (f *skipMcoReboot) GetName() string {
	return "Skip MCO reboot"
}

func (f *skipMcoReboot) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(f, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}
	enableSkipMcoReboot, err := common.BaseVersionGreaterOrEqual("4.15.0", filters.OpenshiftVersion)
	if !enableSkipMcoReboot || err != nil {
		return models.SupportLevelUnavailable
	}
	return models.SupportLevelSupported
}

func (f *skipMcoReboot) getIncompatibleFeatures(openshiftVersion string) *[]models.FeatureSupportLevelID {
	return nil
}

func (f *skipMcoReboot) getIncompatibleArchitectures(openshiftVersion *string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
	}
}

func (f *skipMcoReboot) getFeatureActiveLevel(cluster *common.Cluster, infraEnv *models.InfraEnv,
	clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
	if cluster != nil {
		activeForVersion, err := common.BaseVersionGreaterOrEqual("4.15.0", cluster.OpenshiftVersion)
		if err != nil || !activeForVersion || cluster.CPUArchitecture == models.ClusterCPUArchitectureS390x {
			return activeLevelNotActive
		}
	}
	return activeLevelActive
}
