package featuresupport

import (
	"reflect"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var featuresList = map[models.FeatureSupportLevelID]SupportLevelFeature{
	models.FeatureSupportLevelIDADDITIONALNTPSOURCE:                &AdditionalNtpSourceFeature{},
	models.FeatureSupportLevelIDREQUESTEDHOSTNAME:                  &RequestedHostnameFeature{},
	models.FeatureSupportLevelIDPROXY:                              &ProxyFeature{},
	models.FeatureSupportLevelIDSNO:                                &SnoFeature{},
	models.FeatureSupportLevelIDDAY2HOSTS:                          &Day2HostsFeature{},
	models.FeatureSupportLevelIDVIPAUTOALLOC:                       &VipAutoAllocFeature{},
	models.FeatureSupportLevelIDDISKSELECTION:                      &DiscSelectionFeature{},
	models.FeatureSupportLevelIDOVNNETWORKTYPE:                     &OvnNetworkTypeFeature{},
	models.FeatureSupportLevelIDSDNNETWORKTYPE:                     &SdnNetworkTypeFeature{},
	models.FeatureSupportLevelIDPLATFORMSELECTION:                  &PlatformSelectionFeature{},
	models.FeatureSupportLevelIDSCHEDULABLEMASTERS:                 &SchedulableMastersFeature{},
	models.FeatureSupportLevelIDAUTOASSIGNROLE:                     &AutoAssignRoleFeature{},
	models.FeatureSupportLevelIDCUSTOMMANIFEST:                     &CustomManifestFeature{},
	models.FeatureSupportLevelIDDISKENCRYPTION:                     &DiskEncryptionFeature{},
	models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKINGWITHVMS:    &ClusterManagedNetworkingWithVmsFeature{},
	models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING:           &ClusterManagedNetworkingFeature{},
	models.FeatureSupportLevelIDSINGLENODEEXPANSION:                &SingleNodeExpansionFeature{},
	models.FeatureSupportLevelIDLVM:                                &LvmFeature{},
	models.FeatureSupportLevelIDDUALSTACKNETWORKING:                &DualStackNetworkingFeature{},
	models.FeatureSupportLevelIDNUTANIXINTEGRATION:                 &NutanixIntegrationFeature{},
	models.FeatureSupportLevelIDDUALSTACKVIPS:                      &DualStackVipsFeature{},
	models.FeatureSupportLevelIDUSERMANAGEDNETWORKINGWITHMULTINODE: &UserManagedNetworkingWithMultiNodeFeature{},
}

var cpuFeaturesList = map[models.ArchitectureSupportLevelID]SupportLevelArchitecture{
	models.ArchitectureSupportLevelIDX8664ARCHITECTURE:     &X8664ArchitectureFeature{},
	models.ArchitectureSupportLevelIDARM64ARCHITECTURE:     &Arm64ArchitectureFeature{},
	models.ArchitectureSupportLevelIDS390XARCHITECTURE:     &S390xArchitectureFeature{},
	models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE:   &PPC64LEArchitectureFeature{},
	models.ArchitectureSupportLevelIDMULTIARCHRELEASEIMAGE: &MultiArchReleaseImageFeature{},
}

func getFeatureSupportList(features map[models.FeatureSupportLevelID]SupportLevelFeature, filters SupportLevelFilters) models.SupportLevels {
	featureSupportList := models.SupportLevels{}

	for _, feature := range features {
		if reflect.TypeOf(feature).Name() == "SupportLevelFeature" {
			featureID := feature.GetId()
			featureSupportList[string(featureID)] = feature.GetSupportLevel(filters)
		}
	}
	return featureSupportList
}

func getArchitectureSupportList(features map[models.ArchitectureSupportLevelID]SupportLevelArchitecture, openshiftVersion string) models.SupportLevels {
	featureSupportList := models.SupportLevels{}

	for _, feature := range features {
		featureID := feature.GetId()
		featureSupportList[string(featureID)] = feature.GetSupportLevel(openshiftVersion)
	}
	return featureSupportList
}

func GetFeatureSupportList(openshiftVersion string, cpuArchitecture *string) models.SupportLevels {
	filters := SupportLevelFilters{
		OpenshiftVersion: openshiftVersion,
		CPUArchitecture:  cpuArchitecture,
	}

	if swag.StringValue(filters.CPUArchitecture) == "" {
		filters.CPUArchitecture = swag.String(common.DefaultCPUArchitecture)
	}

	return getFeatureSupportList(featuresList, filters)
}

func GetCpuArchitectureSupportList(openshiftVersion string) models.SupportLevels {
	return getArchitectureSupportList(cpuFeaturesList, openshiftVersion)
}

func GetSupportLevel[T models.FeatureSupportLevelID | models.ArchitectureSupportLevelID](featureId T, filters interface{}) models.SupportLevel {
	if reflect.TypeOf(featureId).Name() == "FeatureSupportLevelID" {
		return featuresList[models.FeatureSupportLevelID(featureId)].GetSupportLevel(filters.(SupportLevelFilters))
	}
	return cpuFeaturesList[models.ArchitectureSupportLevelID(featureId)].GetSupportLevel(filters.(string))
}

func IsFeatureSupported(featureId models.FeatureSupportLevelID, openshiftVersion string, cpuArchitecture *string) bool {
	filters := SupportLevelFilters{
		OpenshiftVersion: openshiftVersion,
		CPUArchitecture:  cpuArchitecture,
	}

	return GetSupportLevel(featureId, filters) == models.SupportLevelSupported
}

func isArchitectureSupported(featureId models.ArchitectureSupportLevelID, openshiftVersion string) bool {
	return GetSupportLevel(featureId, openshiftVersion) == models.SupportLevelSupported
}
