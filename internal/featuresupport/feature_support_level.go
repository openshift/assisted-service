package featuresupport

import (
	"fmt"
	"slices"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var featuresList = map[models.FeatureSupportLevelID]SupportLevelFeature{
	// Generic features
	models.FeatureSupportLevelIDSNO:                       (&SnoFeature{}).New(),
	models.FeatureSupportLevelIDTNA:                       (&TnaFeature{}).New(),
	models.FeatureSupportLevelIDCUSTOMMANIFEST:            (&CustomManifestFeature{}).New(),
	models.FeatureSupportLevelIDSINGLENODEEXPANSION:       (&SingleNodeExpansionFeature{}).New(),
	models.FeatureSupportLevelIDMINIMALISO:                (&MinimalIso{}).New(),
	models.FeatureSupportLevelIDFULLISO:                   (&FullIso{}).New(),
	models.FeatureSupportLevelIDNONSTANDARDHACONTROLPLANE: (&NonStandardHAControlPlane{}).New(),

	// Network features
	models.FeatureSupportLevelIDVIPAUTOALLOC:              (&VipAutoAllocFeature{}).New(),
	models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING:  (&ClusterManagedNetworkingFeature{}).New(),
	models.FeatureSupportLevelIDUSERMANAGEDNETWORKING:     (&UserManagedNetworkingFeature{}).New(),
	models.FeatureSupportLevelIDDUALSTACKVIPS:             (&DualStackVipsFeature{}).New(),
	models.FeatureSupportLevelIDDUALSTACK:                 (&DualStackFeature{}).New(),
	models.FeatureSupportLevelIDPLATFORMMANAGEDNETWORKING: (&PlatformManagedNetworkingFeature{}).New(),
	models.FeatureSupportLevelIDSDNNETWORKTYPE:            (&SDNNetworkTypeFeature{}).New(),
	models.FeatureSupportLevelIDOVNNETWORKTYPE:            (&OVNNetworkTypeFeature{}).New(),
	models.FeatureSupportLevelIDUSERMANAGEDLOADBALANCER:   (&UserManagedLoadBalancerFeature{}).New(),

	// Olm Operators features
	models.FeatureSupportLevelIDLVM:                    (&LvmFeature{}).New(),
	models.FeatureSupportLevelIDCNV:                    (&CnvFeature{}).New(),
	models.FeatureSupportLevelIDLSO:                    (&LsoFeature{}).New(),
	models.FeatureSupportLevelIDMCE:                    (&MceFeature{}).New(),
	models.FeatureSupportLevelIDODF:                    (&OdfFeature{}).New(),
	models.FeatureSupportLevelIDMTV:                    (&MtvFeature{}).New(),
	models.FeatureSupportLevelIDOSC:                    (&OscFeature{}).New(),
	models.FeatureSupportLevelIDNODEFEATUREDISCOVERY:   (&NodeFeatureDiscoveryFeature{}).New(),
	models.FeatureSupportLevelIDNVIDIAGPU:              (&NvidiaGPUFeature{}).New(),
	models.FeatureSupportLevelIDPIPELINES:              (&PipelinesFeature{}).New(),
	models.FeatureSupportLevelIDSERVICEMESH:            (&ServiceMeshFeature{}).New(),
	models.FeatureSupportLevelIDSERVERLESS:             (&ServerLessFeature{}).New(),
	models.FeatureSupportLevelIDOPENSHIFTAI:            (&OpenShiftAIFeature{}).New(),
	models.FeatureSupportLevelIDAUTHORINO:              (&AuthorinoFeature{}).New(),
	models.FeatureSupportLevelIDNMSTATE:                (&NmstateFeature{}).New(),
	models.FeatureSupportLevelIDAMDGPU:                 (&AMDGPUFeature{}).New(),
	models.FeatureSupportLevelIDKMM:                    (&KMMFeature{}).New(),
	models.FeatureSupportLevelIDNODEHEALTHCHECK:        (&NodeHealthcheckFeature{}).New(),
	models.FeatureSupportLevelIDSELFNODEREMEDIATION:    (&SelfNodeRemediationFeature{}).New(),
	models.FeatureSupportLevelIDFENCEAGENTSREMEDIATION: (&FenceAgentsRemediationFeature{}).New(),
	models.FeatureSupportLevelIDNODEMAINTENANCE:        (&NodeMaintenanceFeature{}).New(),
	models.FeatureSupportLevelIDKUBEDESCHEDULER:        (&KubeDeschedulerFeature{}).New(),
	models.FeatureSupportLevelIDCLUSTEROBSERVABILITY:   (&ClusterObservabilityFeature{}).New(),
	models.FeatureSupportLevelIDNUMARESOURCES:          (&NumaResourcesFeature{}).New(),
	models.FeatureSupportLevelIDOADP:                   (&OadpFeature{}).New(),

	// Platform features
	models.FeatureSupportLevelIDNUTANIXINTEGRATION:  (&NutanixIntegrationFeature{}).New(),
	models.FeatureSupportLevelIDVSPHEREINTEGRATION:  (&VsphereIntegrationFeature{}).New(),
	models.FeatureSupportLevelIDEXTERNALPLATFORMOCI: (&OciIntegrationFeature{}).New(),
	models.FeatureSupportLevelIDBAREMETALPLATFORM:   (&BaremetalPlatformFeature{}).New(),
	models.FeatureSupportLevelIDNONEPLATFORM:        (&NonePlatformFeature{}).New(),
	models.FeatureSupportLevelIDEXTERNALPLATFORM:    (&ExternalPlatformFeature{}).New(),
}

func GetFeatureByID(featureID models.FeatureSupportLevelID) SupportLevelFeature {
	return featuresList[featureID]
}

func getFeatureSupportList(features map[models.FeatureSupportLevelID]SupportLevelFeature, filters SupportLevelFilters) []models.Feature {
	ret := make([]models.Feature, 0, len(features))

	for _, feature := range features {
		// skip features that collide with the given filters
		if skipPlatformRelatedFeature(feature.getId(), filters.PlatformType != nil) {
			continue
		}

		supportLevel, reason := feature.getSupportLevel(filters)

		incompatibilities := feature.getIncompatibleFeatures(filters.OpenshiftVersion)
		incompatibilities = filterIncompatibilities(incompatibilities, filters)

		feat := models.Feature{
			FeatureSupportLevelID: feature.getId(),
			Incompatibilities:     incompatibilities,
			SupportLevel:          supportLevel,
			Reason:                reason,
		}

		if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
			feat.SupportLevel = models.SupportLevelUnavailable
			feat.Reason = models.IncompatibilityReasonCPUArchitecture
		}

		ret = append(ret, feat)
	}

	return ret
}

// skipPlatformRelatedFeature return true if
//   - the feature is related to platform and platform type is not set.
//   - the feature is PlatformManagedNetworking and platform type is set.
func skipPlatformRelatedFeature(featureID models.FeatureSupportLevelID, platformTypeSet bool) bool {
	if platformTypeSet && slices.Contains(platformFeatures(), featureID) {
		return true
	}

	// PlatformManagedNetworking is not relevant without platform type - in this case remove disable this feature support-level
	if !platformTypeSet && featureID == models.FeatureSupportLevelIDPLATFORMMANAGEDNETWORKING {
		return true
	}

	return false
}

func filterIncompatibilities(incompatibilities []models.FeatureSupportLevelID, filters SupportLevelFilters) []models.FeatureSupportLevelID {
	ret := make([]models.FeatureSupportLevelID, 0, len(incompatibilities))

	for _, incompatibility := range incompatibilities {
		if skipPlatformRelatedFeature(incompatibility, filters.PlatformType != nil) {
			continue
		}

		ret = append(ret, incompatibility)
	}

	return ret
}

func platformFeatures() []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDNUTANIXINTEGRATION,
		models.FeatureSupportLevelIDVSPHEREINTEGRATION,
		models.FeatureSupportLevelIDEXTERNALPLATFORMOCI,
		models.FeatureSupportLevelIDBAREMETALPLATFORM,
		models.FeatureSupportLevelIDNONEPLATFORM,
		models.FeatureSupportLevelIDEXTERNALPLATFORM,
	}
}

// GetFeatureSupportList Get features support level list, cpuArchitecture is optional and the default value is x86
func GetFeatureSupportList(openshiftVersion string, cpuArchitecture *string, platformType *models.PlatformType, externalPlatformName *string) []models.Feature {
	filters := SupportLevelFilters{
		OpenshiftVersion:     openshiftVersion,
		CPUArchitecture:      cpuArchitecture,
		PlatformType:         platformType,
		ExternalPlatformName: externalPlatformName,
	}

	if cpuArchitecture == nil {
		filters.CPUArchitecture = swag.String(common.DefaultCPUArchitecture)
	}
	featuresSupportList := overrideInvalidRequest(featuresList, *filters.CPUArchitecture, openshiftVersion)
	if featuresSupportList == nil {
		featuresSupportList = getFeatureSupportList(featuresList, filters)
	}

	return featuresSupportList
}

// IsFeatureAvailable Get the support level of a given feature, cpuArchitecture is optional
// with default value of x86_64
func IsFeatureAvailable(featureId models.FeatureSupportLevelID, openshiftVersion string, cpuArchitecture *string) bool {
	filters := SupportLevelFilters{
		OpenshiftVersion: openshiftVersion,
		CPUArchitecture:  cpuArchitecture,
	}

	if cpuArchitecture == nil {
		filters.CPUArchitecture = swag.String(common.DefaultCPUArchitecture)
	}

	return GetSupportLevel(featureId, filters) != models.SupportLevelUnavailable
}

func isFeatureCompatible(openshiftVersion string, feature SupportLevelFeature, features ...SupportLevelFeature) *SupportLevelFeature {
	incompatibilities := feature.getIncompatibleFeatures(openshiftVersion)

	if len(incompatibilities) == 0 {
		return nil
	}

	for _, f := range features {
		if slices.Contains(incompatibilities, f.getId()) {
			return &f
		}
	}

	return nil
}

// isFeaturesCompatibleWithFeatures Determine if feature is compatible with other activated features
func isFeaturesCompatibleWithFeatures(openshiftVersion string, activatedFeatures []SupportLevelFeature) error {
	for _, feature := range activatedFeatures {
		if incompatibleFeature := isFeatureCompatible(openshiftVersion, feature, activatedFeatures...); incompatibleFeature != nil {
			return fmt.Errorf("cannot use %s because it's not compatible with %s", feature.GetName(), (*incompatibleFeature).GetName())
		}
	}

	return nil
}

// isFeaturesCompatible Determine if feature is compatible with CPU architecture in a given openshift-version
func isFeaturesCompatible(openshiftVersion, cpuArchitecture string, activatedFeatures []SupportLevelFeature) error {
	for _, feature := range activatedFeatures {
		if !isFeatureCompatibleWithArchitecture(feature, openshiftVersion, cpuArchitecture) ||
			!IsFeatureAvailable(feature.getId(), openshiftVersion, swag.String(cpuArchitecture)) {
			return fmt.Errorf("cannot use %s because it's not compatible with the %s architecture "+
				"on version %s of OpenShift", feature.GetName(), cpuArchitecture, openshiftVersion)
		}
	}
	return nil
}
