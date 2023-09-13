package featuresupport

import (
	"fmt"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
)

const (
	// supportLevelIgnored - Not an actual support level, this const marks the feature with "ignore" support level
	// so that they can be removed later in case of colliding features (e.g. nutanix and vsphere, sno and HighAvailabilityMode)
	supportLevelIgnored models.SupportLevel = "ignore"
)

var featuresList = map[models.FeatureSupportLevelID]SupportLevelFeature{
	// Generic features
	models.FeatureSupportLevelIDSNO:                 (&SnoFeature{}).New(),
	models.FeatureSupportLevelIDCUSTOMMANIFEST:      (&CustomManifestFeature{}).New(),
	models.FeatureSupportLevelIDSINGLENODEEXPANSION: (&SingleNodeExpansionFeature{}).New(),
	models.FeatureSupportLevelIDMINIMALISO:          (&MinimalIso{}).New(),
	models.FeatureSupportLevelIDFULLISO:             (&FullIso{}).New(),

	// Network features
	models.FeatureSupportLevelIDVIPAUTOALLOC:              (&VipAutoAllocFeature{}).New(),
	models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING:  (&ClusterManagedNetworkingFeature{}).New(),
	models.FeatureSupportLevelIDUSERMANAGEDNETWORKING:     (&UserManagedNetworkingFeature{}).New(),
	models.FeatureSupportLevelIDDUALSTACKVIPS:             (&DualStackVipsFeature{}).New(),
	models.FeatureSupportLevelIDDUALSTACK:                 (&DualStackFeature{}).New(),
	models.FeatureSupportLevelIDPLATFORMMANAGEDNETWORKING: (&PlatformManagedNetworkingFeature{}).New(),

	// Olm Operators features
	models.FeatureSupportLevelIDLVM: (&LvmFeature{}).New(),
	models.FeatureSupportLevelIDCNV: (&CnvFeature{}).New(),
	models.FeatureSupportLevelIDLSO: (&LsoFeature{}).New(),
	models.FeatureSupportLevelIDMCE: (&MceFeature{}).New(),
	models.FeatureSupportLevelIDODF: (&OdfFeature{}).New(),

	// Platform features
	models.FeatureSupportLevelIDNUTANIXINTEGRATION:  (&NutanixIntegrationFeature{}).New(),
	models.FeatureSupportLevelIDVSPHEREINTEGRATION:  (&VsphereIntegrationFeature{}).New(),
	models.FeatureSupportLevelIDEXTERNALPLATFORMOCI: (&OciIntegrationFeature{}).New(),
	models.FeatureSupportLevelIDBAREMETALPLATFORM:   (&BaremetalPlatformFeature{}).New(),
	models.FeatureSupportLevelIDNONEPLATFORM:        (&NonePlatformFeature{}).New(),
}

func GetFeatureByID(featureID models.FeatureSupportLevelID) SupportLevelFeature {
	return featuresList[featureID]
}

func getFeatureSupportList(features map[models.FeatureSupportLevelID]SupportLevelFeature, filters SupportLevelFilters, removeCollidingFeatures bool) models.SupportLevels {
	featureSupportList := models.SupportLevels{}

	for _, feature := range features {
		featureID := feature.getId()

		if !isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture)) {
			featureSupportList[string(featureID)] = models.SupportLevelUnavailable
		} else {
			featureSupportList[string(featureID)] = feature.getSupportLevel(filters, removeCollidingFeatures)
		}
	}
	return featureSupportList
}

// removeEmptySupportLevel remove features with an empty support level value
// Currently in case of filtering features by <platform> we cannot return all other platforms in that list.
func removeEmptySupportLevel(supportLevels models.SupportLevels) {
	var featuresToRemove []string

	for featureId, supportLevel := range supportLevels {
		if supportLevel == supportLevelIgnored {
			featuresToRemove = append(featuresToRemove, featureId)
		}
	}

	for _, featureId := range featuresToRemove {
		delete(supportLevels, featureId)
	}
}

func isPlatformSupported(filters SupportLevelFilters) bool {
	if filters.PlatformType == nil {
		return true
	}

	feature := getPlatformFeatureByType(filters.PlatformType)
	if feature == nil {
		return false
	}
	supportLevel := feature.getSupportLevel(filters, false)
	return supportLevel != models.SupportLevelUnsupported && supportLevel != models.SupportLevelUnavailable
}

func isSnoSupported(filters SupportLevelFilters) bool {
	if filters.HighAvailabilityMode == nil {
		return true
	}

	feature := featuresList[models.FeatureSupportLevelIDSNO]
	supportLevel := feature.getSupportLevel(filters, false)
	return supportLevel != models.SupportLevelUnsupported && supportLevel != models.SupportLevelUnavailable
}

// isValidRequest Checks if the request filters has incompatible parameters (e.g. baremetal platform and SNO)
func isValidRequest(filters SupportLevelFilters) bool {
	if !isArchitectureSupported(filters) {
		return false
	}

	if !isPlatformSupported(filters) || !isSnoSupported(filters) {
		return false
	}

	return true
}

// Handle cases where a CPU architecture is not supported at for a given openshift version, in that case
// return a list of unsupported features
func overrideInvalidRequest(features map[models.FeatureSupportLevelID]SupportLevelFeature, filters SupportLevelFilters) models.SupportLevels {
	supportLevels := models.SupportLevels{}
	if !isValidRequest(filters) {
		for _, feature := range features {
			supportLevels[string(feature.getId())] = models.SupportLevelUnavailable
		}
		return supportLevels
	}
	return nil
}

// GetFeatureSupportList Get features support level list, cpuArchitecture is optional and the default value is x86
func GetFeatureSupportList(filters SupportLevelFilters) models.SupportLevels {
	if filters.CPUArchitecture == nil {
		filters.CPUArchitecture = swag.String(common.DefaultCPUArchitecture)
	}
	featuresSupportList := overrideInvalidRequest(featuresList, filters)
	if featuresSupportList == nil {
		featuresSupportList = getFeatureSupportList(featuresList, filters, true)
	}

	// remove features that collide with the given filters
	removeEmptySupportLevel(featuresSupportList)

	return featuresSupportList
}

// IsFeatureAvailable Get the support level of a given feature, cpuArchitecture is optional
// with default value of x86_64
func IsFeatureAvailable(featureId models.FeatureSupportLevelID, openshiftVersion string, cpuArchitecture *string, highAvailabilityMode *string) bool {
	filters := SupportLevelFilters{
		OpenshiftVersion:     openshiftVersion,
		CPUArchitecture:      cpuArchitecture,
		HighAvailabilityMode: highAvailabilityMode,
	}

	if cpuArchitecture == nil {
		filters.CPUArchitecture = swag.String(common.DefaultCPUArchitecture)
	}

	return GetSupportLevel(featureId, filters) != models.SupportLevelUnavailable
}

func isFeatureCompatible(openshiftVersion string, feature SupportLevelFeature, features ...SupportLevelFeature) *SupportLevelFeature {
	incompatibilities := feature.getIncompatibleFeatures(openshiftVersion)
	if incompatibilities != nil {
		for _, f := range features {
			if funk.Contains(*incompatibilities, f.getId()) {
				return &f
			}
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
func isFeaturesCompatible(openshiftVersion, cpuArchitecture string, highAvailabilityMode *string, activatedFeatures []SupportLevelFeature) error {
	for _, feature := range activatedFeatures {
		if !isFeatureCompatibleWithArchitecture(feature, openshiftVersion, cpuArchitecture) ||
			!IsFeatureAvailable(feature.getId(), openshiftVersion, swag.String(cpuArchitecture), highAvailabilityMode) {
			return fmt.Errorf("cannot use %s because it's not compatible with the %s architecture "+
				"on version %s of OpenShift", feature.GetName(), cpuArchitecture, openshiftVersion)
		}
	}
	return nil
}
