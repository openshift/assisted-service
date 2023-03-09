package featuresupport

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

type SupportLevelFeature interface {
	GetId() models.FeatureSupportLevelID
	GetSupportLevel(filters SupportLevelFilters) models.SupportLevel
}

type SupportLevelFilters struct {
	OpenshiftVersion string
	CPUArchitecture  *string
}

// AdditionalNtpSourceFeature
type AdditionalNtpSourceFeature struct{}

func (feature *AdditionalNtpSourceFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDADDITIONALNTPSOURCE
}

func (feature *AdditionalNtpSourceFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// RequestedHostnameFeature
type RequestedHostnameFeature struct{}

func (feature *RequestedHostnameFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDREQUESTEDHOSTNAME
}

func (feature *RequestedHostnameFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// ProxyFeature
type ProxyFeature struct{}

func (feature *ProxyFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDPROXY
}

func (feature *ProxyFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// SnoFeature
type SnoFeature struct{}

func (feature *SnoFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSNO
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

// Day2HostsFeature
type Day2HostsFeature struct{}

func (feature *Day2HostsFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDDAY2HOSTS
}

func (feature *Day2HostsFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// VipAutoAllocFeature
type VipAutoAllocFeature struct{}

func (feature *VipAutoAllocFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDVIPAUTOALLOC
}

func (feature *VipAutoAllocFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelDevPreview
}

// DiscSelectionFeature
type DiscSelectionFeature struct{}

func (feature *DiscSelectionFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDDISKSELECTION
}

func (feature *DiscSelectionFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// OvnNetworkTypeFeature
type OvnNetworkTypeFeature struct{}

func (feature *OvnNetworkTypeFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDOVNNETWORKTYPE
}

func (feature *OvnNetworkTypeFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// SdnNetworkTypeFeature
type SdnNetworkTypeFeature struct{}

func (feature *SdnNetworkTypeFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSDNNETWORKTYPE
}

func (feature *SdnNetworkTypeFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// PlatformSelectionFeature
type PlatformSelectionFeature struct{}

func (feature *PlatformSelectionFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDPLATFORMSELECTION
}

func (feature *PlatformSelectionFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// SchedulableMastersFeature
type SchedulableMastersFeature struct{}

func (feature *SchedulableMastersFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSCHEDULABLEMASTERS
}

func (feature *SchedulableMastersFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// AutoAssignRoleFeature
type AutoAssignRoleFeature struct{}

func (feature *AutoAssignRoleFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDAUTOASSIGNROLE
}

func (feature *AutoAssignRoleFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// CustomManifestFeature
type CustomManifestFeature struct{}

func (feature *CustomManifestFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDCUSTOMMANIFEST
}

func (feature *CustomManifestFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// DiskEncryptionFeature
type DiskEncryptionFeature struct{}

func (feature *DiskEncryptionFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDDISKENCRYPTION
}

func (feature *DiskEncryptionFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// ClusterManagedNetworkingWithVmsFeature
type ClusterManagedNetworkingWithVmsFeature struct{}

func (feature *ClusterManagedNetworkingWithVmsFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKINGWITHVMS
}

func (feature *ClusterManagedNetworkingWithVmsFeature) GetSupportLevel(_ SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}

// ClusterManagedNetworkingFeature
type ClusterManagedNetworkingFeature struct{}

func (feature *ClusterManagedNetworkingFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING
}

func (feature *ClusterManagedNetworkingFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if swag.StringValue(filters.CPUArchitecture) == models.ClusterCPUArchitectureArm64 {
		isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion)
		if isNotSupported || err != nil {
			return models.SupportLevelUnsupported
		}
	}

	return models.SupportLevelSupported
}

// SingleNodeExpansionFeature
type SingleNodeExpansionFeature struct{}

func (feature *SingleNodeExpansionFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDSINGLENODEEXPANSION
}

func (feature *SingleNodeExpansionFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion)
	if isNotSupported || err != nil {
		return models.SupportLevelUnsupported
	}

	return models.SupportLevelSupported
}

// LvmFeature
type LvmFeature struct{}

func (feature *LvmFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDLVM
}

func (feature *LvmFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnsupported
	}

	if isEqual, _ := common.BaseVersionEqual("4.11", filters.OpenshiftVersion); isEqual {
		return models.SupportLevelDevPreview
	}

	return models.SupportLevelSupported
}

// DualStackNetworkingFeature
type DualStackNetworkingFeature struct{}

func (feature *DualStackNetworkingFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDDUALSTACKNETWORKING
}

func (feature *DualStackNetworkingFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if isNotSupported, err := common.BaseVersionLessThan("4.8", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnsupported
	}

	return models.SupportLevelSupported
}

// NutanixIntegrationFeature
type NutanixIntegrationFeature struct{}

func (feature *NutanixIntegrationFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNUTANIXINTEGRATION
}

func (feature *NutanixIntegrationFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if isNotSupported, err := common.BaseVersionLessThan("4.11", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnsupported
	}

	if isEqual, _ := common.BaseVersionEqual("4.11", filters.OpenshiftVersion); isEqual {
		return models.SupportLevelDevPreview
	}
	return models.SupportLevelSupported
}

// DualStackVipsFeature
type DualStackVipsFeature struct{}

func (feature *DualStackVipsFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDDUALSTACKVIPS
}

func (feature *DualStackVipsFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	if isNotSupported, err := common.BaseVersionLessThan("4.12", filters.OpenshiftVersion); isNotSupported || err != nil {
		return models.SupportLevelUnsupported
	}

	return models.SupportLevelSupported
}

// UserManagedNetworkingWithMultiNodeFeature
type UserManagedNetworkingWithMultiNodeFeature struct{}

func (feature *UserManagedNetworkingWithMultiNodeFeature) GetId() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDUSERMANAGEDNETWORKINGWITHMULTINODE
}

func (feature *UserManagedNetworkingWithMultiNodeFeature) GetSupportLevel(filters SupportLevelFilters) models.SupportLevel {
	return models.SupportLevelSupported
}
