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
	vipsFeature VipsFeature
}

func (feature *ClusterManagedNetworkingFeature) New() SupportLevelFeature {
	return &ClusterManagedNetworkingFeature{
		VipsFeature{},
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

	if filters.PlatformType != nil {
		if *filters.PlatformType == models.PlatformTypeOci || *filters.PlatformType == models.PlatformTypeNone {
			return models.SupportLevelUnavailable
		}
	}

	return models.SupportLevelSupported
}

func areVipsSet(cluster *common.Cluster, clusterUpdateParams *models.V2ClusterUpdateParams) bool {
	if clusterUpdateParams != nil && clusterUpdateParams.APIVips != nil {
		if len(clusterUpdateParams.APIVips) > 0 {
			return true
		} else if len(clusterUpdateParams.APIVips) == 0 {
			return false
		}
	}

	if cluster != nil && len(cluster.APIVips) > 0 {
		return true
	}

	return false
}

func (feature *ClusterManagedNetworkingFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if cluster == nil {
		return activeLevelNotActive
	}

	if cluster.Platform != nil {
		if IsActivePlatformSupportsCmn(cluster, clusterUpdateParams) {
			// Specifically on vSphere platform, because both umn and cmn available check if VIPs are set
			if isPlatformActive(cluster, clusterUpdateParams, models.PlatformTypeVsphere) {
				if feature.vipsFeature.getFeatureActiveLevel(cluster, nil, clusterUpdateParams, nil) != activeLevelActive {
					return activeLevelNotActive
				}
			}
			return activeLevelActive
		}
		return activeLevelNotActive
	}

	return activeLevelNotRelevant
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
type UserManagedNetworkingFeature struct {
	vipsFeature VipsFeature
}

func (feature *UserManagedNetworkingFeature) New() SupportLevelFeature {
	return &UserManagedNetworkingFeature{
		vipsFeature: VipsFeature{},
	}
}

func (feature *UserManagedNetworkingFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDUSERMANAGEDNETWORKING
}

func (feature *UserManagedNetworkingFeature) GetName() string {
	return "User Managed Networking"
}

func (feature *UserManagedNetworkingFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if filters.PlatformType != nil {
		if *filters.PlatformType == models.PlatformTypeNutanix || *filters.PlatformType == models.PlatformTypeBaremetal {
			return models.SupportLevelUnavailable
		}
	}

	return models.SupportLevelSupported
}

func (feature *UserManagedNetworkingFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDBAREMETALPLATFORM,
		models.FeatureSupportLevelIDVIPS,
	}
}

func (feature *UserManagedNetworkingFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *UserManagedNetworkingFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if cluster == nil {
		return activeLevelNotActive
	}

	if cluster.Platform != nil {
		if IsActivePlatformSupportsUmn(cluster, clusterUpdateParams) {
			// Specifically on vSphere platform, because both umn and cmn available check if VIPs are set
			if isPlatformActive(cluster, clusterUpdateParams, models.PlatformTypeVsphere) {
				if feature.vipsFeature.getFeatureActiveLevel(cluster, nil, clusterUpdateParams, nil) == activeLevelActive {
					return activeLevelNotActive
				}
			}
			return activeLevelActive
		}
		return activeLevelNotActive
	}

	return activeLevelNotRelevant
}

// VIPs
type VipsFeature struct{}

func (feature *VipsFeature) New() SupportLevelFeature {
	return &VipsFeature{}
}

func (feature *VipsFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDVIPS
}

func (feature *VipsFeature) GetName() string {
	return "API and Ingress VIPs"
}

func (feature *VipsFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if filters.PlatformType != nil {
		if *filters.PlatformType == models.PlatformTypeOci || *filters.PlatformType == models.PlatformTypeNone {
			return models.SupportLevelUnavailable
		}
	}

	return models.SupportLevelSupported
}

func (feature *VipsFeature) getIncompatibleFeatures(string) *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDUSERMANAGEDNETWORKING,
	}
}

func (feature *VipsFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *VipsFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if cluster == nil {
		return activeLevelNotActive
	}

	// assuming that all VIPs must be updated together so no need to test also Ingress VIP
	if clusterUpdateParams != nil && clusterUpdateParams.APIVips != nil {
		if len(clusterUpdateParams.APIVips) > 0 {
			return activeLevelActive
		}

		if len(clusterUpdateParams.APIVips) == 0 {
			return activeLevelNotActive
		}
	}

	if cluster.APIVips != nil && len(cluster.APIVips) > 0 {
		return activeLevelActive
	}
	return activeLevelNotActive
}
