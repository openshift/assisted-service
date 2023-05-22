package featuresupport

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/models"
)

func getFeatureSupportLevelPtr(supportLevel models.SupportLevel) *models.SupportLevel {
	return &supportLevel
}

var SupportLevelsList = models.FeatureSupportLevels{
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.9",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSNO),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			// Dev-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelDevPreview),
			},
			// Unsupported features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSINGLENODEEXPANSION),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDLVM),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.10",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSNO),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			// Dev-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelDevPreview),
			},
			// Unsupported features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSINGLENODEEXPANSION),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDLVM),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.11",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSNO),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSINGLENODEEXPANSION),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			// Tech-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelTechPreview),
			},
			// Dev-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelDevPreview),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDLVM),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelDevPreview),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelDevPreview),
			},
			// Unsupported features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.12",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSNO),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSINGLENODEEXPANSION),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDLVM),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			// Tech-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelTechPreview),
			},
			// Dev-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelDevPreview),
			},
			// Unsupported features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelUnsupported),
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.13",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSNO),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDSINGLENODEEXPANSION),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDLVM),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKNETWORKING),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDNUTANIXINTEGRATION),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDDUALSTACKVIPS),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDPPC64LEARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDS390XARCHITECTURE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelSupported),
			},
			// Tech-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDMULTIARCHRELEASEIMAGE),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelTechPreview),
			},
			// Dev-Preview features
			{
				FeatureID:    swag.String(models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC),
				SupportLevel: getFeatureSupportLevelPtr(models.SupportLevelDevPreview),
			},
			// Unsupported features
		},
	},
}
