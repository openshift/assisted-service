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
	// New - Initialize new SupportLevelFeature structure while setting its default attributes
	New() SupportLevelFeature
	// getId - Get SupportLevelFeature unique ID
	getId() models.FeatureSupportLevelID
	// getName - Get SupportLevelFeature user friendly name
	getName() string
	// getSupportLevel - Get feature support-level value, filtered by given filters (e.g. OpenshiftVersion, CpuArchitecture)
	getSupportLevel(filters SupportLevelFilters) models.SupportLevel
	// getIncompatibleFeatures - Get a list of features that cannot exist alongside this feature
	getIncompatibleFeatures() *[]models.FeatureSupportLevelID
	// getIncompatibleArchitectures - Get a list of architectures which the given feature will not work on
	getIncompatibleArchitectures(openshiftVersion *string) *[]models.ArchitectureSupportLevelID
	// getFeatureActiveLevel - Get the feature status, if it's active, not-active or not relevant (in cases where there is no meaning for that feature to be active)
	getFeatureActiveLevel(cluster *common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel
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

func isOperatorActivated(operator string, cluster *common.Cluster, updateParams *models.V2ClusterUpdateParams) bool {
	if cluster == nil {
		return false
	}
	activeOperators, updatedOperators := getOperatorsList(*cluster, updateParams)
	operatorActivated := activeOperators != nil && (funk.Contains(*activeOperators, operator))
	operatorUpdated := updatedOperators != nil && (funk.Contains(*updatedOperators, operator))

	return (operatorActivated && (updateParams == nil || updateParams.OlmOperators == nil)) || operatorActivated && operatorUpdated || operatorUpdated
}

// SnoFeature
type SnoFeature struct{}

func (feature *SnoFeature) New() SupportLevelFeature {
	return &SnoFeature{}
}

func (feature *SnoFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSNO
}

func (feature *SnoFeature) getName() string {
	return "Single Node OpenShift"
}

func (feature *SnoFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}
	if isNotSupported, err := common.BaseVersionLessThan("4.8", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable
	}
	if isEqual, _ := common.BaseVersionEqual("4.8", filters.OpenshiftVersion); isEqual {
		return models.SupportLevelDevPreview
	}

	if swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureS390x || swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitecturePpc64le {
		if isEqual, _ := common.BaseVersionEqual("4.13", filters.OpenshiftVersion); isEqual {
			return models.SupportLevelDevPreview
		}
	}

	return models.SupportLevelSupported
}

func (feature *SnoFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
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

// VipAutoAllocFeature
type VipAutoAllocFeature struct{}

func (feature *VipAutoAllocFeature) New() SupportLevelFeature {
	return &VipAutoAllocFeature{}
}

func (feature *VipAutoAllocFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDVIPAUTOALLOC
}

func (feature *VipAutoAllocFeature) getName() string {
	return "VIP Automatic Allocation"
}

func (feature *VipAutoAllocFeature) getSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelDevPreview
}

func (feature *VipAutoAllocFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return nil
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

// CustomManifestFeature
type CustomManifestFeature struct{}

func (feature *CustomManifestFeature) New() SupportLevelFeature {
	return &CustomManifestFeature{}
}

func (feature *CustomManifestFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDCUSTOMMANIFEST
}

func (feature *CustomManifestFeature) getName() string {
	return "Custom Manifest"
}

func (feature *CustomManifestFeature) getSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

func (feature *CustomManifestFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *CustomManifestFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *CustomManifestFeature) getFeatureActiveLevel(_ *common.Cluster, _ *models.InfraEnv, _ *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	return activeLeveNotRelevant
}

// ClusterManagedNetworkingFeature
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

func (feature *ClusterManagedNetworkingFeature) getName() string {
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

func (feature *ClusterManagedNetworkingFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDUSERMANAGEDNETWORKING,
	}
}

// DualStackVipsFeature
type DualStackVipsFeature struct{}

func (feature *DualStackVipsFeature) New() SupportLevelFeature {
	return &DualStackVipsFeature{}
}

func (feature *DualStackVipsFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDDUALSTACKVIPS
}

func (feature *DualStackVipsFeature) getName() string {
	return "Dual-Stack VIPs"
}

func (feature *DualStackVipsFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}
	if isNotSupported, err := common.BaseVersionLessThan("4.12", filters.OpenshiftVersion); isNotSupported || err != nil {
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

func (feature *DualStackVipsFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return nil
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

func (feature *UserManagedNetworkingFeature) getName() string {
	return "User Managed Networking"
}

func (feature *UserManagedNetworkingFeature) getSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

func (feature *UserManagedNetworkingFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
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

func (feature *SingleNodeExpansionFeature) getName() string {
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

func (feature *SingleNodeExpansionFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *SingleNodeExpansionFeature) getIncompatibleArchitectures(openshiftVersion *string) *[]models.ArchitectureSupportLevelID {
	return feature.snoFeature.getIncompatibleArchitectures(openshiftVersion)
}

// LvmFeature
type LvmFeature struct{}

func (feature *LvmFeature) New() SupportLevelFeature {
	return &LvmFeature{}
}

func (feature *LvmFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDLVM
}

func (feature *LvmFeature) getName() string {
	return "Logical Volume Management"
}

func (feature *LvmFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
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

func (feature *LvmFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("lvm", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *LvmFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDVIPAUTOALLOC,
		models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
		models.FeatureSupportLevelIDODF,
	}
}

func (feature *LvmFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

// NutanixIntegrationFeature
type NutanixIntegrationFeature struct{}

func (feature *NutanixIntegrationFeature) New() SupportLevelFeature {
	return &NutanixIntegrationFeature{}
}

func (feature *NutanixIntegrationFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNUTANIXINTEGRATION
}

func (feature *NutanixIntegrationFeature) getName() string {
	return "Nutanix Platform Integration"
}

func (feature *NutanixIntegrationFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
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
	if cluster == nil {
		return activeLevelNotActive
	}

	if (cluster.Platform != nil && common.PlatformTypeValue(cluster.Platform.Type) == models.PlatformTypeNutanix && clusterUpdateParams == nil) ||
		(cluster.Platform != nil && common.PlatformTypeValue(cluster.Platform.Type) == models.PlatformTypeNutanix && clusterUpdateParams != nil && (clusterUpdateParams.Platform == nil || common.PlatformTypeValue(clusterUpdateParams.Platform.Type) == models.PlatformTypeNutanix)) ||
		((cluster.Platform != nil && common.PlatformTypeValue(cluster.Platform.Type) != models.PlatformTypeNutanix) && clusterUpdateParams != nil && (clusterUpdateParams.Platform != nil && common.PlatformTypeValue(clusterUpdateParams.Platform.Type) == models.PlatformTypeNutanix)) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *NutanixIntegrationFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDUSERMANAGEDNETWORKING,
		models.FeatureSupportLevelIDLVM,
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

func (feature *VsphereIntegrationFeature) getName() string {
	return "vSphere Platform Integration"
}

func (feature *VsphereIntegrationFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *VsphereIntegrationFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if cluster == nil {
		return activeLevelNotActive
	}

	if (cluster.Platform != nil && common.PlatformTypeValue(cluster.Platform.Type) == models.PlatformTypeVsphere && clusterUpdateParams == nil) ||
		(cluster.Platform != nil && common.PlatformTypeValue(cluster.Platform.Type) == models.PlatformTypeVsphere && clusterUpdateParams != nil && (clusterUpdateParams.Platform == nil || common.PlatformTypeValue(clusterUpdateParams.Platform.Type) == models.PlatformTypeVsphere)) ||
		(cluster.Platform != nil && common.PlatformTypeValue(cluster.Platform.Type) != models.PlatformTypeVsphere && clusterUpdateParams != nil && (clusterUpdateParams.Platform != nil && common.PlatformTypeValue(clusterUpdateParams.Platform.Type) == models.PlatformTypeVsphere)) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *VsphereIntegrationFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return &[]models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDLVM,
	}
}

func (feature *VsphereIntegrationFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
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

func (feature *OdfFeature) getName() string {
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

func (feature *OdfFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
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

func (feature *CnvFeature) getName() string {
	return "OpenShift Virtualization"
}

func (feature *CnvFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *CnvFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return nil
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

func (feature *LsoFeature) getName() string {
	return "Local Storage Operator"
}

func (feature *LsoFeature) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *LsoFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *LsoFeature) getIncompatibleArchitectures(_ *string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	}
}

func (feature *LsoFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("lso", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// MinimalIso
type MinimalIso struct{}

func (feature *MinimalIso) New() SupportLevelFeature {
	return &MinimalIso{}
}

func (feature *MinimalIso) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDMINIMALISO
}

func (feature *MinimalIso) getName() string {
	return "Minimal ISO"
}

func (feature *MinimalIso) getSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable
	}

	return models.SupportLevelSupported
}

func (feature *MinimalIso) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
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
