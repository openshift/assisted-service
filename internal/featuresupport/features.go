package featuresupport

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
)

type featureActiveLevel string

const (
	activeLevelActive     featureActiveLevel = "Active"
	activeLevelNotActive  featureActiveLevel = "NotActive"
	activeLeveNotRelevant featureActiveLevel = "NotRelevant"
)

type SupportLevelFeature interface {
	GetId() models.FeatureSupportLevelID
	GetName() string
	GetSupportLevel(filters SupportLevelFilters) models.SupportLevel
	GetIncompatibleFeatures() *[]models.FeatureSupportLevelID
	GetIncompatibleArchitectures(openshiftVersion string) *[]models.ArchitectureSupportLevelID
	getFeatureActiveLevel(cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) featureActiveLevel
}

type SupportLevelFilters struct {
	OpenshiftVersion string
	CPUArchitecture  *string
}

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

func isOperatorActivated(operator string, cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) bool {
	activeOperators, updatedOperators := getOperatorsList(cluster, updateParams)
	operatorActivated := activeOperators != nil && (funk.Contains(*activeOperators, operator))
	operatorUpdated := updatedOperators != nil && (funk.Contains(*updatedOperators, operator))

	return (operatorActivated && (updateParams == nil || updateParams.OlmOperators == nil)) || operatorActivated && operatorUpdated || operatorUpdated
}

// SnoFeature
type SnoFeature struct{}

func (feature *SnoFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSNO
}

func (feature *SnoFeature) GetName() string {
	return "Single Node OpenShift"
}

func (feature *SnoFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if isNotSupported, err := common.BaseVersionLessThan("4.8", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnsupported
	}
	if isEqual, _ := common.BaseVersionEqual("4.8", filters.OpenshiftVersion); isEqual {
		return models.SupportLevelDevPreview
	}

	return models.SupportLevelSupported
}

func (feature *SnoFeature) GetIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDODF,
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
		models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDVIPAUTOALLOC,
	}
}

func (feature *SnoFeature) GetIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

func (feature *SnoFeature) getFeatureActiveLevel(cluster common.Cluster, _ *models.V2ClusterUpdateParams) featureActiveLevel {
	if swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// VipAutoAllocFeature
type VipAutoAllocFeature struct{}

func (feature *VipAutoAllocFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDVIPAUTOALLOC
}

func (feature *VipAutoAllocFeature) GetName() string {
	return "VIP Automatic Allocation"
}

func (feature *VipAutoAllocFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelDevPreview
}

func (feature *VipAutoAllocFeature) GetIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *VipAutoAllocFeature) GetIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *VipAutoAllocFeature) getFeatureActiveLevel(cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) featureActiveLevel {
	if (swag.BoolValue(cluster.VipDhcpAllocation) && updateParams == nil) ||
		(swag.BoolValue(cluster.VipDhcpAllocation) && updateParams != nil && (updateParams.VipDhcpAllocation == nil || *updateParams.VipDhcpAllocation)) ||
		(!swag.BoolValue(cluster.VipDhcpAllocation) && updateParams != nil && updateParams.VipDhcpAllocation != nil && *updateParams.VipDhcpAllocation) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// CustomManifestFeature
type CustomManifestFeature struct{}

func (feature *CustomManifestFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDCUSTOMMANIFEST
}

func (feature *CustomManifestFeature) GetName() string {
	return "Custom Manifest"
}

func (feature *CustomManifestFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

func (feature *CustomManifestFeature) GetIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *CustomManifestFeature) GetIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *CustomManifestFeature) getFeatureActiveLevel(_ common.Cluster, _ *models.V2ClusterUpdateParams) featureActiveLevel {
	return activeLeveNotRelevant
}

// ClusterManagedNetworkingFeature
type ClusterManagedNetworkingFeature struct{}

func (feature *ClusterManagedNetworkingFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING
}

func (feature *ClusterManagedNetworkingFeature) GetName() string {
	return "Cluster Managed Networking"
}

func (feature *ClusterManagedNetworkingFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureS390x || swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitecturePpc64le {
		return models.SupportLevelUnsupported
	}

	if swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureArm64 {
		isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion)
		if isNotSupported || err != nil {
			return models.SupportLevelUnsupported
		}
	}

	return models.SupportLevelSupported
}

func (feature *ClusterManagedNetworkingFeature) getFeatureActiveLevel(cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) featureActiveLevel {
	if (!swag.BoolValue(cluster.UserManagedNetworking) && updateParams == nil) ||
		(!swag.BoolValue(cluster.UserManagedNetworking) && updateParams != nil && !swag.BoolValue(updateParams.UserManagedNetworking)) ||
		(swag.BoolValue(cluster.UserManagedNetworking) && updateParams != nil && updateParams.UserManagedNetworking != nil && !*updateParams.UserManagedNetworking) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *ClusterManagedNetworkingFeature) GetIncompatibleArchitectures(openshiftVersion string) *[]models.ArchitectureSupportLevelID {
	incompatibilities := []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}

	if isGreater, _ := common.BaseVersionGreaterOrEqual("4.11", openshiftVersion); isGreater {
		return &incompatibilities
	}
	incompatibilities = append(incompatibilities, models.ArchitectureSupportLevelIDARM64ARCHITECTURE)
	return &incompatibilities
}

func (feature *ClusterManagedNetworkingFeature) GetIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDUSERMANAGEDNETWORKING,
	}
}

// DualStackVipsFeature
type DualStackVipsFeature struct{}

func (feature *DualStackVipsFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDDUALSTACKVIPS
}

func (feature *DualStackVipsFeature) GetName() string {
	return "Dual-Stack VIPs"
}

func (feature *DualStackVipsFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if isNotSupported, err := common.BaseVersionLessThan("4.12", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnsupported
	}

	return models.SupportLevelSupported
}

func (feature *DualStackVipsFeature) getFeatureActiveLevel(cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) featureActiveLevel {
	if (cluster.APIVips != nil && len(cluster.APIVips) > 1 && updateParams == nil) ||
		(cluster.APIVips != nil && len(cluster.APIVips) > 1 && updateParams != nil && (updateParams.APIVips == nil || len(updateParams.APIVips) > 1)) ||
		(cluster.APIVips != nil && len(cluster.APIVips) <= 1 && updateParams != nil && updateParams.APIVips != nil && len(updateParams.APIVips) > 1) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *DualStackVipsFeature) GetIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *DualStackVipsFeature) GetIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return nil
}

// UserManagedNetworkingFeature
type UserManagedNetworkingFeature struct{}

func (feature *UserManagedNetworkingFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDUSERMANAGEDNETWORKING
}

func (feature *UserManagedNetworkingFeature) GetName() string {
	return "User Managed Networking"
}

func (feature *UserManagedNetworkingFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

func (feature *UserManagedNetworkingFeature) GetIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
	}
}

func (feature *UserManagedNetworkingFeature) GetIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *UserManagedNetworkingFeature) getFeatureActiveLevel(cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) featureActiveLevel {
	if (swag.BoolValue(cluster.UserManagedNetworking) && updateParams == nil) ||
		(swag.BoolValue(cluster.UserManagedNetworking) && updateParams != nil && (updateParams.UserManagedNetworking == nil || *updateParams.UserManagedNetworking)) ||
		(!swag.BoolValue(cluster.UserManagedNetworking) && updateParams != nil && updateParams.UserManagedNetworking != nil && *updateParams.UserManagedNetworking) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// SingleNodeExpansionFeature
type SingleNodeExpansionFeature struct{}

func (feature *SingleNodeExpansionFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSINGLENODEEXPANSION
}

func (feature *SingleNodeExpansionFeature) GetName() string {
	return "Single Node Expansion"
}

func (feature *SingleNodeExpansionFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion)
	if isNotSupported || err != nil {
		return models.SupportLevelUnsupported
	}

	return models.SupportLevelSupported
}

func (feature *SingleNodeExpansionFeature) getFeatureActiveLevel(_ common.Cluster, _ *models.V2ClusterUpdateParams) featureActiveLevel {
	return activeLeveNotRelevant
}

func (feature *SingleNodeExpansionFeature) GetIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *SingleNodeExpansionFeature) GetIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

// LvmFeature
type LvmFeature struct{}

func (feature *LvmFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDLVM
}

func (feature *LvmFeature) GetName() string {
	return "Logical Volume Management"
}

func (feature *LvmFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureS390x || swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitecturePpc64le {
		return models.SupportLevelUnsupported
	}

	if isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnsupported
	}

	if isEqual, _ := common.BaseVersionEqual("4.11", filters.OpenshiftVersion); isEqual {
		return models.SupportLevelDevPreview
	}

	return models.SupportLevelSupported
}

func (feature *LvmFeature) getFeatureActiveLevel(cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) featureActiveLevel {
	if isOperatorActivated("lvm", cluster, updateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *LvmFeature) GetIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDVIPAUTOALLOC,
		models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
		models.FeatureSupportLevelIDODF,
	}
}

func (feature *LvmFeature) GetIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

// NutanixIntegrationFeature
type NutanixIntegrationFeature struct{}

func (feature *NutanixIntegrationFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNUTANIXINTEGRATION
}

func (feature *NutanixIntegrationFeature) GetName() string {
	return "Nutanix Platform Integration"
}

func (feature *NutanixIntegrationFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureS390x ||
		swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitecturePpc64le ||
		swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureArm64 {
		return models.SupportLevelUnsupported
	}

	if isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnsupported
	}

	if isEqual, _ := common.BaseVersionEqual("4.11", filters.OpenshiftVersion); isEqual {
		return models.SupportLevelDevPreview
	}
	return models.SupportLevelSupported
}

func (feature *NutanixIntegrationFeature) getFeatureActiveLevel(cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) featureActiveLevel {
	if (cluster.Platform != nil && *cluster.Platform.Type == models.PlatformTypeNutanix && updateParams == nil) ||
		(cluster.Platform != nil && *cluster.Platform.Type == models.PlatformTypeNutanix && updateParams != nil && (updateParams.Platform == nil || *updateParams.Platform.Type == models.PlatformTypeNutanix)) ||
		((cluster.Platform != nil && *cluster.Platform.Type != models.PlatformTypeNutanix) && updateParams != nil && (updateParams.Platform != nil && *updateParams.Platform.Type == models.PlatformTypeNutanix)) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *NutanixIntegrationFeature) GetIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDUSERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDLVM,
	}
}

func (feature *NutanixIntegrationFeature) GetIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	}
}

// VsphereIntegrationFeature
type VsphereIntegrationFeature struct{}

func (feature *VsphereIntegrationFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDVSPHEREINTEGRATION
}

func (feature *VsphereIntegrationFeature) GetName() string {
	return "vSphere Platform Integration"
}

func (feature *VsphereIntegrationFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureS390x || swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitecturePpc64le {
		return models.SupportLevelUnsupported
	}
	return models.SupportLevelSupported
}

func (feature *VsphereIntegrationFeature) getFeatureActiveLevel(cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) featureActiveLevel {
	if (cluster.Platform != nil && *cluster.Platform.Type == models.PlatformTypeVsphere && updateParams == nil) ||
		(cluster.Platform != nil && *cluster.Platform.Type == models.PlatformTypeVsphere && updateParams != nil && (updateParams.Platform == nil || *updateParams.Platform.Type == models.PlatformTypeVsphere)) ||
		(cluster.Platform != nil && *cluster.Platform.Type != models.PlatformTypeVsphere && updateParams != nil && (updateParams.Platform != nil && *updateParams.Platform.Type == models.PlatformTypeVsphere)) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *VsphereIntegrationFeature) GetIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDLVM,
	}
}

func (feature *VsphereIntegrationFeature) GetIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

// OdfFeature
type OdfFeature struct{}

func (feature *OdfFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDODF
}

func (feature *OdfFeature) GetName() string {
	return "OpenShift Data Foundation"
}

func (feature *OdfFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureArm64 {
		return models.SupportLevelUnsupported
	}

	return models.SupportLevelSupported
}

func (feature *OdfFeature) GetIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	}
}

func (feature *OdfFeature) GetIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDLVM,
	}
}

func (feature *OdfFeature) getFeatureActiveLevel(cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) featureActiveLevel {
	if isOperatorActivated("odf", cluster, updateParams) || isOperatorActivated("ocs", cluster, updateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// CnvFeature
type CnvFeature struct{}

func (feature *CnvFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDCNV
}

func (feature *CnvFeature) GetName() string {
	return "OpenShift Virtualization"
}

func (feature *CnvFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureX8664 {
		return models.SupportLevelSupported
	}

	return models.SupportLevelUnsupported
}

func (feature *CnvFeature) GetIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *CnvFeature) GetIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	}
}

func (feature *CnvFeature) getFeatureActiveLevel(cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) featureActiveLevel {
	if isOperatorActivated("cnv", cluster, updateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}
