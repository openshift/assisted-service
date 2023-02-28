package featuresupport

import (
	"fmt"

	"github.com/go-openapi/swag"
	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
)

func usageNameToID(key string) string {
	return usage.UsageNameToID(key)
}

var SupportLevelsList = models.FeatureSupportLevels{
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.6",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Dev-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview),
			},
			// Unsupported features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSNO),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSINGLENODEEXPANSION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDLVM),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.7",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Unsupported features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.8",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			// Dev-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSNO),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview),
			},
			// Unsupported features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSINGLENODEEXPANSION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDLVM),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.9",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSNO),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			// Dev-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview),
			},
			// Unsupported features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSINGLENODEEXPANSION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDLVM),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.10",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSNO),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			// Dev-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview),
			},
			// Unsupported features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSINGLENODEEXPANSION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDLVM),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.11",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSNO),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSINGLENODEEXPANSION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			// Tech-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelTechPreview),
			},
			// Dev-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDLVM),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview),
			},
			// Unsupported features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.12",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSNO),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSINGLENODEEXPANSION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDLVM),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			// Tech-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelTechPreview),
			},
			// Dev-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview),
			},
			// Unsupported features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported),
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.13",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSNO),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSINGLENODEEXPANSION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDLVM),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelSupported),
			},
			// Tech-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelTechPreview),
			},
			// Dev-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC),
				SupportLevel: swag.String(models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview),
			},
			// Unsupported features
		},
	},
}

// default is supported
func GetFeatureSupportLevel(openshiftVersion string, featureId string) string {
	openshiftVersion, err := getVersionKey(openshiftVersion)
	if err != nil {
		return models.FeatureSupportLevelFeaturesItems0SupportLevelSupported
	}
	for _, supportLevel := range SupportLevelsList {
		if supportLevel.OpenshiftVersion == openshiftVersion {
			for _, feature := range supportLevel.Features {
				if usageNameToID(featureId) == *feature.FeatureID {
					return *feature.SupportLevel
				}
			}
			break
		}
	}
	return models.FeatureSupportLevelFeaturesItems0SupportLevelSupported
}

func IsFeatureSupported(openshiftVersion string, featureId string) bool {
	return GetFeatureSupportLevel(openshiftVersion, featureId) == models.FeatureSupportLevelFeaturesItems0SupportLevelSupported
}

func getVersionKey(openshiftVersion string) (string, error) {
	v, err := version.NewVersion(openshiftVersion)
	if err != nil {
		return openshiftVersion, err
	}

	// put string in x.y format
	return fmt.Sprintf("%d.%d", v.Segments()[0], v.Segments()[1]), nil
}
