package featuresupport

import (
	"slices"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/clusterobservability"
	"github.com/openshift/assisted-service/internal/operators/fenceagentsremediation"
	"github.com/openshift/assisted-service/internal/operators/kubedescheduler"
	"github.com/openshift/assisted-service/internal/operators/nodehealthcheck"
	"github.com/openshift/assisted-service/internal/operators/nodemaintenance"
	"github.com/openshift/assisted-service/internal/operators/numaresources"
	"github.com/openshift/assisted-service/internal/operators/oadp"
	"github.com/openshift/assisted-service/internal/operators/selfnoderemediation"
	"github.com/openshift/assisted-service/models"
)

func getOperatorsList(cluster common.Cluster, updateParams *models.V2ClusterUpdateParams) ([]string, []string) {
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

	return clusterOperators, updateParamsOperators
}

func isOperatorActivated(operator string, cluster *common.Cluster, updateParams *models.V2ClusterUpdateParams) bool {
	if cluster == nil {
		return false
	}
	activeOperators, updatedOperators := getOperatorsList(*cluster, updateParams)
	operatorActivated := slices.Contains(activeOperators, operator)
	operatorUpdated := slices.Contains(updatedOperators, operator)

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

func (feature *LvmFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	if isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonOpenshiftVersion
	}

	if filters.PlatformType != nil && (*filters.PlatformType == models.PlatformTypeVsphere || *filters.PlatformType == models.PlatformTypeNutanix) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonPlatform
	}

	if isEqual, _ := common.BaseVersionEqual("4.11", filters.OpenshiftVersion); isEqual {
		return models.SupportLevelDevPreview, ""
	}

	return models.SupportLevelSupported, ""
}

func (feature *LvmFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("lvm", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

func (feature *LvmFeature) getIncompatibleFeatures(OCPVersion string) []models.FeatureSupportLevelID {
	incompatibleFeatures := []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
		models.FeatureSupportLevelIDODF,
		models.FeatureSupportLevelIDOPENSHIFTAI,
	}
	if isEqual, _ := common.BaseVersionLessThan("4.15", OCPVersion); isEqual {
		incompatibleFeatures = append(incompatibleFeatures,
			models.FeatureSupportLevelIDVIPAUTOALLOC,
			models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
		)
	}
	return incompatibleFeatures
}

func (feature *LvmFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{
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

func (feature *OdfFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	if filters.PlatformType != nil && *filters.PlatformType == models.PlatformTypeExternal &&
		swag.StringValue(filters.ExternalPlatformName) == common.ExternalPlatformNameOci {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonPlatform
	}

	return models.SupportLevelSupported, ""
}

func (feature *OdfFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	}
}

func (feature *OdfFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDLVM,
		models.FeatureSupportLevelIDEXTERNALPLATFORMOCI,
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

func (feature *CnvFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	if filters.PlatformType != nil && (*filters.PlatformType == models.PlatformTypeNutanix || *filters.PlatformType == models.PlatformTypeVsphere) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonPlatform
	}

	if models.ArchitectureSupportLevelIDARM64ARCHITECTURE == cpuArchitectureFeatureIdMap[swag.StringValue(filters.CPUArchitecture)] {
		return models.SupportLevelDevPreview, ""
	}

	return models.SupportLevelSupported, ""
}

func (feature *CnvFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
	}
}

func (feature *CnvFeature) getIncompatibleArchitectures(OCPVersion *string) []models.ArchitectureSupportLevelID {
	incompatibleArchitecture := []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
	if isLessThan, _ := common.BaseVersionLessThan("4.14", *OCPVersion); isLessThan {
		incompatibleArchitecture = append(incompatibleArchitecture, models.ArchitectureSupportLevelIDARM64ARCHITECTURE)
	}

	return incompatibleArchitecture
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

func (feature *LsoFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	return models.SupportLevelSupported, ""
}

func (feature *LsoFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return nil
}

func (feature *LsoFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{
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

func (feature *MceFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	isNotSupported, err := common.BaseVersionLessThan("4.10", filters.OpenshiftVersion)
	if isNotSupported || err != nil {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonOpenshiftVersion
	}

	if filters.PlatformType != nil && (*filters.PlatformType == models.PlatformTypeNutanix) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonPlatform
	}

	return models.SupportLevelSupported, ""
}

func (feature *MceFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
	}
}

func (feature *MceFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return nil
}

func (feature *MceFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("mce", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// MtvFeature
type MtvFeature struct{}

func (feature *MtvFeature) New() SupportLevelFeature {
	return &MtvFeature{}
}

func (feature *MtvFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDMTV
}

func (feature *MtvFeature) GetName() string {
	return "OpenShift Migration Toolkit for Virtualization"
}

func (feature *MtvFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	if filters.PlatformType != nil && (*filters.PlatformType == models.PlatformTypeVsphere || *filters.PlatformType == models.PlatformTypeNutanix) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonPlatform
	}

	if isNotSupported, err := common.BaseVersionLessThan("4.14", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonOpenshiftVersion
	}

	return models.SupportLevelSupported, ""
}

func (feature *MtvFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	incompatibleArchitecture := []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
	return incompatibleArchitecture
}

func (feature *MtvFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
	}
}

func (feature *MtvFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("mtv", cluster, clusterUpdateParams) && isOperatorActivated("cnv", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// NodeFeatureDiscoveryFeature describes the support for the node feature discovery operator.
type NodeFeatureDiscoveryFeature struct{}

func (f *NodeFeatureDiscoveryFeature) New() SupportLevelFeature {
	return &NodeFeatureDiscoveryFeature{}
}

func (f *NodeFeatureDiscoveryFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNODEFEATUREDISCOVERY
}

func (f *NodeFeatureDiscoveryFeature) GetName() string {
	return "Node Feature Discovery"
}

func (f *NodeFeatureDiscoveryFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(f, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	if isNotSupported, err := common.BaseVersionLessThan("4.6", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonOpenshiftVersion
	}

	return models.SupportLevelDevPreview, ""
}

func (f *NodeFeatureDiscoveryFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
	}
}

func (f *NodeFeatureDiscoveryFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{}
}

func (f *NodeFeatureDiscoveryFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("node-feature-discovery", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// NvidiaGPUFeature describes the support for the NVIDIA GPU operator.
type NvidiaGPUFeature struct{}

func (f *NvidiaGPUFeature) New() SupportLevelFeature {
	return &NvidiaGPUFeature{}
}

func (f *NvidiaGPUFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNVIDIAGPU
}

func (f *NvidiaGPUFeature) GetName() string {
	return "NVIDIA GPU"
}

func (f *NvidiaGPUFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(f, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	return models.SupportLevelDevPreview, ""
}

func (f *NvidiaGPUFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
	}
}

func (f *NvidiaGPUFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{}
}

func (f *NvidiaGPUFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("nvidia-gpu", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// PipelinesFeature describes the support for the pipelines operator.
type PipelinesFeature struct{}

func (f *PipelinesFeature) New() SupportLevelFeature {
	return &PipelinesFeature{}
}

func (f *PipelinesFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDPIPELINES
}

func (f *PipelinesFeature) GetName() string {
	return "Pipelines"
}

func (f *PipelinesFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(f, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	return models.SupportLevelDevPreview, ""
}

func (f *PipelinesFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{}
}

func (f *PipelinesFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{}
}

func (f *PipelinesFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("pipelines", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// ServiceMeshFeature describes the support for the service mesh operator.
type ServiceMeshFeature struct{}

func (f *ServiceMeshFeature) New() SupportLevelFeature {
	return &ServiceMeshFeature{}
}

func (f *ServiceMeshFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSERVICEMESH
}

func (f *ServiceMeshFeature) GetName() string {
	return "ServiceMesh"
}

func (f *ServiceMeshFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(f, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	return models.SupportLevelDevPreview, ""
}

func (f *ServiceMeshFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{}
}

func (f *ServiceMeshFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{}
}

func (f *ServiceMeshFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("servicemesh", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// ServerLessFeature describes the support for the serverless operator.
type ServerLessFeature struct{}

func (f *ServerLessFeature) New() SupportLevelFeature {
	return &ServerLessFeature{}
}

func (f *ServerLessFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSERVERLESS
}

func (f *ServerLessFeature) GetName() string {
	return "ServerLess"
}

func (f *ServerLessFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(f, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	return models.SupportLevelDevPreview, ""
}

func (f *ServerLessFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{}
}

func (f *ServerLessFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{}
}

func (f *ServerLessFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("serverless", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// OpenShiftAPIFeature describes the support for the OpenShift API operator.
type OpenShiftAIFeature struct{}

func (f *OpenShiftAIFeature) New() SupportLevelFeature {
	return &OpenShiftAIFeature{}
}

func (f *OpenShiftAIFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDOPENSHIFTAI
}

func (f *OpenShiftAIFeature) GetName() string {
	return "OpenShift AI"
}

func (f *OpenShiftAIFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(f, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	if isNotSupported, err := common.BaseVersionLessThan("4.12", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonOpenshiftVersion
	}

	if filters.PlatformType != nil && *filters.PlatformType == models.PlatformTypeExternal &&
		swag.StringValue(filters.ExternalPlatformName) == common.ExternalPlatformNameOci {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonPlatform
	}

	return models.SupportLevelDevPreview, ""
}

func (f *OpenShiftAIFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
	}
}

func (f *OpenShiftAIFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		// These aren't directly incompatible with OpenShift AI, rather with ODF, but the feature support
		// mechanism doesn't currently understand operator dependencies, so we need to add these explicitly.
		models.FeatureSupportLevelIDLVM,
		models.FeatureSupportLevelIDSNO,
		models.FeatureSupportLevelIDEXTERNALPLATFORMOCI,
	}
}

func (f *OpenShiftAIFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("openshift-ai", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// AuthorinoFeature describes the support for the Authorino operator.
type AuthorinoFeature struct{}

func (f *AuthorinoFeature) New() SupportLevelFeature {
	return &AuthorinoFeature{}
}

func (f *AuthorinoFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDAUTHORINO
}

func (f *AuthorinoFeature) GetName() string {
	return "Authorino"
}

func (f *AuthorinoFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	return models.SupportLevelDevPreview, ""
}

func (f *AuthorinoFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
	}
}

func (f *AuthorinoFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return nil
}

func (f *AuthorinoFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("authorino", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// OscFeature
type OscFeature struct{}

func (feature *OscFeature) New() SupportLevelFeature {
	return &OscFeature{}
}

func (feature *OscFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDOSC
}

func (feature *OscFeature) GetName() string {
	return "OpenShift sandboxed containers"
}

func (feature *OscFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	if filters.PlatformType != nil && (*filters.PlatformType == models.PlatformTypeVsphere || *filters.PlatformType == models.PlatformTypeNutanix) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonPlatform
	}

	if isNotSupported, err := common.BaseVersionLessThan("4.10", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonOpenshiftVersion
	}

	return models.SupportLevelTechPreview, ""
}

func (feature *OscFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	incompatibleArchitecture := []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
	return incompatibleArchitecture
}

func (feature *OscFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
	}
}

func (feature *OscFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("osc", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// NmstateFeature
type NmstateFeature struct{}

func (feature *NmstateFeature) New() SupportLevelFeature {
	return &NmstateFeature{}
}

func (feature *NmstateFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNMSTATE
}

func (feature *NmstateFeature) GetName() string {
	return "Nmstate node network configuration"
}

func (feature *NmstateFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	if filters.PlatformType != nil && (*filters.PlatformType == models.PlatformTypeNutanix || *filters.PlatformType == models.PlatformTypeExternal) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonPlatform
	}

	if isNotSupported, err := common.BaseVersionLessThan("4.12", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonOpenshiftVersion
	}

	return models.SupportLevelSupported, ""
}

func (feature *NmstateFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
	}
}

func (feature *NmstateFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDEXTERNALPLATFORM,
		models.FeatureSupportLevelIDEXTERNALPLATFORMOCI,
	}
}

func (feature *NmstateFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("nmstate", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// AMDGPUFeature describes the support for the AMD GPU operator.
type AMDGPUFeature struct{}

func (f *AMDGPUFeature) New() SupportLevelFeature {
	return &AMDGPUFeature{}
}

func (f *AMDGPUFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDAMDGPU
}

func (f *AMDGPUFeature) GetName() string {
	return "AMD GPU"
}

func (f *AMDGPUFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(f, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	return models.SupportLevelDevPreview, ""
}

func (f *AMDGPUFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{
		models.ArchitectureSupportLevelIDARM64ARCHITECTURE,
		models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE,
		models.ArchitectureSupportLevelIDS390XARCHITECTURE,
	}
}

func (f *AMDGPUFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{}
}

func (f *AMDGPUFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("amd-gpu", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// KMMFeature describes the support for the KMM operator.
type KMMFeature struct{}

func (f *KMMFeature) New() SupportLevelFeature {
	return &KMMFeature{}
}

func (f *KMMFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDKMM
}

func (f *KMMFeature) GetName() string {
	return "Kernel Module Management"
}

func (f *KMMFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	if !isFeatureCompatibleWithArchitecture(f, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
		return models.SupportLevelUnavailable, models.IncompatibilityReasonCPUArchitecture
	}

	return models.SupportLevelDevPreview, ""
}

func (f *KMMFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{}
}

func (f *KMMFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{}
}

func (f *KMMFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated("kmm", cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// NodeHealthcheckFeature describes the support for the Node Healthcheck operator.
type NodeHealthcheckFeature struct{}

func (f *NodeHealthcheckFeature) New() SupportLevelFeature {
	return &NodeHealthcheckFeature{}
}

func (f *NodeHealthcheckFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNODEHEALTHCHECK
}

func (f *NodeHealthcheckFeature) GetName() string {
	return nodehealthcheck.OperatorFullName
}

func (f *NodeHealthcheckFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	return models.SupportLevelTechPreview, ""
}

func (f *NodeHealthcheckFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{}
}

func (f *NodeHealthcheckFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
	}
}

func (f *NodeHealthcheckFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated(nodehealthcheck.Operator.Name, cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// SelfNodeRemediationFeature describes the support for the Self Node Remediation operator.
type SelfNodeRemediationFeature struct{}

func (f *SelfNodeRemediationFeature) New() SupportLevelFeature {
	return &SelfNodeRemediationFeature{}
}

func (f *SelfNodeRemediationFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSELFNODEREMEDIATION
}

func (f *SelfNodeRemediationFeature) GetName() string {
	return selfnoderemediation.OperatorFullName
}

func (f *SelfNodeRemediationFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	return models.SupportLevelTechPreview, ""
}

func (f *SelfNodeRemediationFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{}
}

func (f *SelfNodeRemediationFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
	}
}

func (f *SelfNodeRemediationFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated(selfnoderemediation.Operator.Name, cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// FenceAgentsRemediationFeature describes the support for the Fence Agents Remediation operator.
type FenceAgentsRemediationFeature struct{}

func (f *FenceAgentsRemediationFeature) New() SupportLevelFeature {
	return &FenceAgentsRemediationFeature{}
}

func (f *FenceAgentsRemediationFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDFENCEAGENTSREMEDIATION
}

func (f *FenceAgentsRemediationFeature) GetName() string {
	return fenceagentsremediation.OperatorFullName
}

func (f *FenceAgentsRemediationFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	return models.SupportLevelTechPreview, ""
}

func (f *FenceAgentsRemediationFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{}
}

func (f *FenceAgentsRemediationFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
	}
}

func (f *FenceAgentsRemediationFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated(fenceagentsremediation.Operator.Name, cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// NodeMaintenanceFeature describes the support for the Node Maintenance Operator.
type NodeMaintenanceFeature struct{}

func (f *NodeMaintenanceFeature) New() SupportLevelFeature {
	return &NodeMaintenanceFeature{}
}

func (f *NodeMaintenanceFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNODEMAINTENANCE
}

func (f *NodeMaintenanceFeature) GetName() string {
	return fenceagentsremediation.OperatorFullName
}

func (f *NodeMaintenanceFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	return models.SupportLevelTechPreview, ""
}

func (f *NodeMaintenanceFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{}
}

func (f *NodeMaintenanceFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
	}
}

func (f *NodeMaintenanceFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated(nodemaintenance.Operator.Name, cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// KubeDeschedulerFeature describes the support for the Kube Descheduler Operator.
type KubeDeschedulerFeature struct{}

func (f *KubeDeschedulerFeature) New() SupportLevelFeature {
	return &KubeDeschedulerFeature{}
}

func (f *KubeDeschedulerFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDKUBEDESCHEDULER
}

func (f *KubeDeschedulerFeature) GetName() string {
	return fenceagentsremediation.OperatorFullName
}

func (f *KubeDeschedulerFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	return models.SupportLevelTechPreview, ""
}

func (f *KubeDeschedulerFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{}
}

func (f *KubeDeschedulerFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDSNO,
	}
}

func (f *KubeDeschedulerFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated(kubedescheduler.Operator.Name, cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// ClusterObservabilityFeature describes the support for the Cluster Observability Operator.
type ClusterObservabilityFeature struct{}

func (f *ClusterObservabilityFeature) New() SupportLevelFeature {
	return &ClusterObservabilityFeature{}
}

func (f *ClusterObservabilityFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDCLUSTEROBSERVABILITY
}

func (f *ClusterObservabilityFeature) GetName() string {
	return clusterobservability.OperatorFullName
}

func (f *ClusterObservabilityFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	return models.SupportLevelTechPreview, ""
}

func (f *ClusterObservabilityFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{}
}

func (f *ClusterObservabilityFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{}
}

func (f *ClusterObservabilityFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated(clusterobservability.Operator.Name, cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// NumaResourcesFeature describes the support for the NUMA Resources operator.
type NumaResourcesFeature struct{}

func (f *NumaResourcesFeature) New() SupportLevelFeature {
	return &NumaResourcesFeature{}
}

func (f *NumaResourcesFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNUMARESOURCES
}

func (f *NumaResourcesFeature) GetName() string {
	return numaresources.OperatorFullName
}

func (f *NumaResourcesFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	return models.SupportLevelTechPreview, ""
}

func (f *NumaResourcesFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{}
}

func (f *NumaResourcesFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{}
}

func (f *NumaResourcesFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated(numaresources.Operator.Name, cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}

// OadpFeature describes the support for the OADP operator.
type OadpFeature struct{}

func (f *OadpFeature) New() SupportLevelFeature {
	return &OadpFeature{}
}

func (f *OadpFeature) getId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDOADP
}

func (f *OadpFeature) GetName() string {
	return oadp.OperatorFullName
}

func (f *OadpFeature) getSupportLevel(filters SupportLevelFilters) (models.SupportLevel, models.IncompatibilityReason) {
	return models.SupportLevelTechPreview, ""
}

func (f *OadpFeature) getIncompatibleArchitectures(_ *string) []models.ArchitectureSupportLevelID {
	return []models.ArchitectureSupportLevelID{}
}

func (f *OadpFeature) getIncompatibleFeatures(string) []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{}
}

func (f *OadpFeature) getFeatureActiveLevel(cluster *common.Cluster, _ *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, _ *models.InfraEnvUpdateParams) featureActiveLevel {
	if isOperatorActivated(oadp.Operator.Name, cluster, clusterUpdateParams) {
		return activeLevelActive
	}
	return activeLevelNotActive
}
