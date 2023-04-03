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
	getId() models.FeatureSupportLevelID
	getName() string
	getSupportLevel(filters SupportLevelFilters) models.SupportLevel
	getIncompatibleFeatures() *[]models.FeatureSupportLevelID
	getIncompatibleArchitectures(openshiftVersion string) *[]models.ArchitectureSupportLevelID
	getFeatureActiveLevel(cluster common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel
}

type SupportLevelFilters struct {
	OpenshiftVersion string
	CPUArchitecture  *string
}

func isFeatureCompatibleWithArchitecture(feature SupportLevelFeature, openshiftVersion, cpuArchitecture string) bool {
	architectureID := cpuArchitectureFeatureIdMap[cpuArchitecture]
	incompatibilitiesArchitectures := feature.getIncompatibleArchitectures(openshiftVersion)
	if incompatibilitiesArchitectures != nil && funk.Contains(*incompatibilitiesArchitectures, architectureID) {
		return false
	}
	return true
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

func (feature *SnoFeature) getIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

func (feature *SnoFeature) getFeatureActiveLevel(cluster common.Cluster, infraEnv *models.InfraEnv, _ *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// VipAutoAllocFeature
type VipAutoAllocFeature struct{}

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

func (feature *VipAutoAllocFeature) getIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *VipAutoAllocFeature) getFeatureActiveLevel(cluster common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
	if (swag.BoolValue(cluster.VipDhcpAllocation) && clusterUpdateParams == nil) ||
		(swag.BoolValue(cluster.VipDhcpAllocation) && clusterUpdateParams != nil && (clusterUpdateParams.VipDhcpAllocation == nil || *clusterUpdateParams.VipDhcpAllocation)) ||
		(!swag.BoolValue(cluster.VipDhcpAllocation) && clusterUpdateParams != nil && clusterUpdateParams.VipDhcpAllocation != nil && *clusterUpdateParams.VipDhcpAllocation) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// CustomManifestFeature
type CustomManifestFeature struct{}

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

func (feature *CustomManifestFeature) getIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *CustomManifestFeature) getFeatureActiveLevel(_ common.Cluster, _ *models.InfraEnv, _ *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	return activeLeveNotRelevant
}

// ClusterManagedNetworkingFeature
type ClusterManagedNetworkingFeature struct{}

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

func (feature *ClusterManagedNetworkingFeature) getFeatureActiveLevel(cluster common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
	if (!swag.BoolValue(cluster.UserManagedNetworking) && clusterUpdateParams == nil) ||
		(!swag.BoolValue(cluster.UserManagedNetworking) && clusterUpdateParams != nil && !swag.BoolValue(clusterUpdateParams.UserManagedNetworking)) ||
		(swag.BoolValue(cluster.UserManagedNetworking) && clusterUpdateParams != nil && clusterUpdateParams.UserManagedNetworking != nil && !*clusterUpdateParams.UserManagedNetworking) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *ClusterManagedNetworkingFeature) getIncompatibleArchitectures(openshiftVersion string) *[]models.ArchitectureSupportLevelID {
	incompatibilities := []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
	}

	if isGreater, _ := common.BaseVersionGreaterOrEqual("4.11", openshiftVersion); isGreater {
		return &incompatibilities
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

func (feature *DualStackVipsFeature) getFeatureActiveLevel(cluster common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
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

func (feature *DualStackVipsFeature) getIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return nil
}

// UserManagedNetworkingFeature
type UserManagedNetworkingFeature struct{}

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

func (feature *UserManagedNetworkingFeature) getIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return nil
}

func (feature *UserManagedNetworkingFeature) getFeatureActiveLevel(cluster common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
	if (swag.BoolValue(cluster.UserManagedNetworking) && clusterUpdateParams == nil) ||
		(swag.BoolValue(cluster.UserManagedNetworking) && clusterUpdateParams != nil && (clusterUpdateParams.UserManagedNetworking == nil || *clusterUpdateParams.UserManagedNetworking)) ||
		(!swag.BoolValue(cluster.UserManagedNetworking) && clusterUpdateParams != nil && clusterUpdateParams.UserManagedNetworking != nil && *clusterUpdateParams.UserManagedNetworking) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// SingleNodeExpansionFeature
type SingleNodeExpansionFeature struct{}

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

	return models.SupportLevelSupported
}

func (feature *SingleNodeExpansionFeature) getFeatureActiveLevel(_ common.Cluster, _ *models.InfraEnv, _ *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	return activeLeveNotRelevant
}

func (feature *SingleNodeExpansionFeature) getIncompatibleFeatures() *[]models.FeatureSupportLevelID {
	return nil
}

func (feature *SingleNodeExpansionFeature) getIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

// LvmFeature
type LvmFeature struct{}

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

func (feature *LvmFeature) getFeatureActiveLevel(cluster common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
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

func (feature *LvmFeature) getIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

// NutanixIntegrationFeature
type NutanixIntegrationFeature struct{}

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

func (feature *NutanixIntegrationFeature) getFeatureActiveLevel(cluster common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
	if (cluster.Platform != nil && *cluster.Platform.Type == models.PlatformTypeNutanix && clusterUpdateParams == nil) ||
		(cluster.Platform != nil && *cluster.Platform.Type == models.PlatformTypeNutanix && clusterUpdateParams != nil && (clusterUpdateParams.Platform == nil || *clusterUpdateParams.Platform.Type == models.PlatformTypeNutanix)) ||
		((cluster.Platform != nil && *cluster.Platform.Type != models.PlatformTypeNutanix) && clusterUpdateParams != nil && (clusterUpdateParams.Platform != nil && *clusterUpdateParams.Platform.Type == models.PlatformTypeNutanix)) {
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

func (feature *NutanixIntegrationFeature) getIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	}
}

// VsphereIntegrationFeature
type VsphereIntegrationFeature struct{}

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

func (feature *VsphereIntegrationFeature) getFeatureActiveLevel(cluster common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
	if (cluster.Platform != nil && *cluster.Platform.Type == models.PlatformTypeVsphere && clusterUpdateParams == nil) ||
		(cluster.Platform != nil && *cluster.Platform.Type == models.PlatformTypeVsphere && clusterUpdateParams != nil && (clusterUpdateParams.Platform == nil || *clusterUpdateParams.Platform.Type == models.PlatformTypeVsphere)) ||
		(cluster.Platform != nil && *cluster.Platform.Type != models.PlatformTypeVsphere && clusterUpdateParams != nil && (clusterUpdateParams.Platform != nil && *clusterUpdateParams.Platform.Type == models.PlatformTypeVsphere)) {
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

func (feature *VsphereIntegrationFeature) getIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

// OdfFeature
type OdfFeature struct{}

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

func (feature *OdfFeature) getIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
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

func (feature *OdfFeature) getFeatureActiveLevel(cluster common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("odf", cluster, clusterUpdateParams) || isOperatorActivated("ocs", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// CnvFeature
type CnvFeature struct{}

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

func (feature *CnvFeature) getIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	}
}

func (feature *CnvFeature) getFeatureActiveLevel(cluster common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("cnv", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// MinimalIso
type MinimalIso struct{}

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

func (feature *MinimalIso) getIncompatibleArchitectures(_ string) *[]models.ArchitectureSupportLevelID {
	return &[]models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
	}
}

func (feature *MinimalIso) getFeatureActiveLevel(cluster common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel {
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
