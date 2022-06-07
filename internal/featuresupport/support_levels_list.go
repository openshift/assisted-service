package featuresupport

import (
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
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
			},
			// Unsupported features
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDSNO,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDCLUSTERMANAGEDNETWORKINGWITHVMS,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.8",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Dev-Preview features
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
			},
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDSNO,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
			},

			// Unsupported features
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDCLUSTERMANAGEDNETWORKINGWITHVMS,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.9",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDSNO,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelSupported,
			},
			// Dev-Preview features
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
			},
			// Unsupported features
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDCLUSTERMANAGEDNETWORKINGWITHVMS,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.10",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDSNO,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelSupported,
			},
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelSupported,
			},
			// Dev-Preview features
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
			},
			// Unsupported features
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDCLUSTERMANAGEDNETWORKINGWITHVMS,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.11",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDSNO,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelSupported,
			},
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTURE,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelSupported,
			},
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelSupported,
			},
			// Dev-Preview features
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDVIPAUTOALLOC,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
			},
			// Unsupported features
			{
				FeatureID:    models.FeatureSupportLevelFeaturesItems0FeatureIDCLUSTERMANAGEDNETWORKINGWITHVMS,
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
		},
	},
}

//default is supported
func GetFeatureSupportLevel(openshiftVersion string, featureId string) string {
	for _, supportLevel := range SupportLevelsList {
		if supportLevel.OpenshiftVersion == openshiftVersion {
			for _, feature := range supportLevel.Features {
				if usageNameToID(featureId) == feature.FeatureID {
					return feature.SupportLevel
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
