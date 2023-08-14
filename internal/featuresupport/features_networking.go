package featuresupport

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
)

// VipAutoAllocFeature
type VipAutoAllocFeature struct{}

func (feature *VipAutoAllocFeature) New() SupportLevelFeature {
	return &VipAutoAllocFeature{}
}

func (feature *VipAutoAllocFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDVIPAUTOALLOC
}

func (feature *VipAutoAllocFeature) GetName() string {
	return "VIP Automatic Allocation"
}

func (feature *VipAutoAllocFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if filters.PlatformType != nil && *filters.PlatformType == models.PlatformTypeOci {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelDevPreview
}

func (feature *VipAutoAllocFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDEXTERNALPLATFORMOCI,
		models.FeatureSupportLevelIDLVM,
		models.FeatureSupportLevelIDNONEPLATFORM,
	}
}

func (feature *VipAutoAllocFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *VipAutoAllocFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if cluster == nil {
		return activeLevelNotActive
	}

	if (swag.BoolValue(cluster.VipDhcpAllocation) && clusterUpdateParams == nil) ||
		(swag.BoolValue(cluster.VipDhcpAllocation) && clusterUpdateParams != nil && (clusterUpdateParams.VipDhcpAllocation == nil || *clusterUpdateParams.VipDhcpAllocation)) ||
		(!swag.BoolValue(cluster.VipDhcpAllocation) && clusterUpdateParams != nil && clusterUpdateParams.VipDhcpAllocation != nil && *clusterUpdateParams.VipDhcpAllocation) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// ClusterManagedNetworkingFeature - DEPRECATED
type ClusterManagedNetworkingFeature struct {
	umnFeature UserManagedNetworkingFeature
}

func (feature *ClusterManagedNetworkingFeature) New() SupportLevelFeature {
	return &ClusterManagedNetworkingFeature{
		UserManagedNetworkingFeature{},
	}
}

func (feature *ClusterManagedNetworkingFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING
}

func (feature *ClusterManagedNetworkingFeature) GetName() string {
	return "Cluster Managed Networking"
}

func (feature *ClusterManagedNetworkingFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}
	if swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureArm64 {
		isNotAvailable, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion)
		if isNotAvailable || err != nil {
			return models.SupportLevelUnavailable
		}
	}

	if filters.PlatformType != nil && *filters.PlatformType == models.PlatformTypeOci {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *ClusterManagedNetworkingFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if cluster == nil {
		return activeLevelNotActive
	}

	if !feature.umnFeature.isFeatureActive(cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *ClusterManagedNetworkingFeature) getIncompatibleArchitectures(openshiftVersion *string) *[]models.ArchitectureSupportLevelID {
	incompatibilities := []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}

	if openshiftVersion != nil {
		if isGreater, _ := common.BaseVersionGreaterOrEqual("4.11", *openshiftVersion); isGreater {
			return &incompatibilities
		}
	}

	incompatibilities = append(incompatibilities, models.ArchitectureSupportLevelIDARM64ARCHITECTURE)
	return &incompatibilities
}

func (feature *ClusterManagedNetworkingFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDUSERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDEXTERNALPLATFORMOCI,
		models.FeatureSupportLevelIDLVM,
		models.FeatureSupportLevelIDNONEPLATFORM,
	}
}

// DualStackFeature
type DualStackFeature struct{}

func (feature *DualStackFeature) New() SupportLevelFeature {
	return &DualStackFeature{}
}

func (feature *DualStackFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDDUALSTACK
}

func (feature *DualStackFeature) GetName() string {
	return "Dual-Stack"
}

func (feature *DualStackFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *DualStackFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if clusterUpdateParams != nil {
		if network.CheckIfNetworksAreDualStack(clusterUpdateParams.MachineNetworks, clusterUpdateParams.ServiceNetworks, clusterUpdateParams.ClusterNetworks) {
			return activeLevelActive
		}
	}

	if network.CheckIfClusterIsDualStack(cluster) {
		return activeLevelActive
	}

	return activeLevelNotActive
}

func (feature *DualStackFeature) getIncompatibleFeatures(openshiftVersion string) *[]models.FeatureSupportLevelID {
	if isNotSupported, err := common.BaseVersionLessThan("4.13", openshiftVersion); isNotSupported || err != nil {
		return &[]models.FeatureSupportLevelID{
			models.FeatureSupportLevelIDVSPHEREINTEGRATION,
		}
	}
	return nil
}

func (feature *DualStackFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

// DualStackVipsFeature
type DualStackVipsFeature struct{}

func (feature *DualStackVipsFeature) New() SupportLevelFeature {
	return &DualStackVipsFeature{}
}

func (feature *DualStackVipsFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDDUALSTACKVIPS
}

func (feature *DualStackVipsFeature) GetName() string {
	return "Dual-Stack VIPs"
}

func (feature *DualStackVipsFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}
	if isNotSupported, err := common.BaseVersionLessThan("4.12", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable
	}

	if filters.PlatformType != nil && *filters.PlatformType == models.PlatformTypeOci {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *DualStackVipsFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if cluster == nil {
		return activeLevelNotActive
	}
	if (cluster.APIVips != nil && len(cluster.APIVips) > 1 && clusterUpdateParams == nil) ||
		(cluster.APIVips != nil && len(cluster.APIVips) > 1 && clusterUpdateParams != nil && (clusterUpdateParams.APIVips == nil || len(clusterUpdateParams.APIVips) > 1)) ||
		(cluster.APIVips != nil && len(cluster.APIVips) <= 1 && clusterUpdateParams != nil && clusterUpdateParams.APIVips != nil && len(clusterUpdateParams.APIVips) > 1) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *DualStackVipsFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDEXTERNALPLATFORMOCI,
	}
}

func (feature *DualStackVipsFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

// UserManagedNetworkingFeature
type UserManagedNetworkingFeature struct{}

func (feature *UserManagedNetworkingFeature) New() SupportLevelFeature {
	return &UserManagedNetworkingFeature{}
}

func (feature *UserManagedNetworkingFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDUSERMANAGEDNETWORKING
}

func (feature *UserManagedNetworkingFeature) GetName() string {
	return "User Managed Networking"
}

func (feature *UserManagedNetworkingFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if filters.PlatformType != nil && *filters.PlatformType == models.PlatformTypeNutanix {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *UserManagedNetworkingFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDBAREMETALPLATFORM,
	}
}

func (feature *UserManagedNetworkingFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *UserManagedNetworkingFeature) isFeatureActive(cluster *common.Cluster, clusterUpdateParams *models.V2ClusterUpdateParams) bool {
	// Check if User Managed Networking is active for a given cluster when passing update params:
	// 1. If the cluster UMN is enabled and the update params are empty
	// 2. If the cluster UMN is enabled and enabled in the update params or not set at all
	// 3. If the cluster UMN is disabled and enabled in the update params
	if (swag.BoolValue(cluster.UserManagedNetworking) && clusterUpdateParams == nil) ||
		(swag.BoolValue(cluster.UserManagedNetworking) && clusterUpdateParams != nil && (clusterUpdateParams.UserManagedNetworking == nil || *clusterUpdateParams.UserManagedNetworking)) ||
		(!swag.BoolValue(cluster.UserManagedNetworking) && clusterUpdateParams != nil && clusterUpdateParams.UserManagedNetworking != nil && *clusterUpdateParams.UserManagedNetworking) {
		return true
	}
	return false
}

func (feature *UserManagedNetworkingFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if cluster == nil {
		return activeLevelNotActive
	}

	if feature.isFeatureActive(cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// PlatformManagedNetworkingFeature
type PlatformManagedNetworkingFeature struct{}

func (feature *PlatformManagedNetworkingFeature) New() SupportLevelFeature {
	return &PlatformManagedNetworkingFeature{}
}

func (feature *PlatformManagedNetworkingFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDPLATFORMMANAGEDNETWORKING
}

func (feature *PlatformManagedNetworkingFeature) GetName() string {
	return "Platform managed networking"
}

func (feature *PlatformManagedNetworkingFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	// PlatformManagedNetworking is not relevant without platform type - in this case remove disable this feature support-level
	if !isPlatformSet(filters) {
		return ""
	}

	if filters.PlatformType != nil && (*filters.PlatformType == models.PlatformTypeOci || *filters.PlatformType == models.PlatformTypeNone) {
		return models.SupportLevelSupported
	}

	return models.SupportLevelUnsupported
}

func (feature *PlatformManagedNetworkingFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDBAREMETALPLATFORM,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
	}
}

func (feature *PlatformManagedNetworkingFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *PlatformManagedNetworkingFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isPlatformActive(cluster, clusterUpdateParams, models.PlatformTypeNone) || isPlatformActive(cluster, clusterUpdateParams, models.PlatformTypeOci) {
		return activeLevelActive
	}

	return activeLevelNotActive
}
