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
			// Tech-Preview features
			{
				FeatureID:    usageNameToID(usage.VipDhcpAllocationUsage),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelTechPreview,
			},
			// Unsupported features
			{
				FeatureID:    usageNameToID(usage.HighAvailabilityModeUsage),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
			{
				FeatureID:    usageNameToID(usage.CPUArchitectureARM64),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
			{
				FeatureID:    usageNameToID(usage.UserManagedNetworkWithVMs),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.8",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Tech-Preview features
			{
				FeatureID:    usageNameToID(usage.VipDhcpAllocationUsage),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelTechPreview,
			},
			// Dev-Preview features
			{
				FeatureID:    usageNameToID(usage.HighAvailabilityModeUsage),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
			},

			// Unsupported features
			{
				FeatureID:    usageNameToID(usage.CPUArchitectureARM64),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
			{
				FeatureID:    usageNameToID(usage.UserManagedNetworkWithVMs),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.9",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    usageNameToID(usage.HighAvailabilityModeUsage),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelSupported,
			},
			// Tech-Preview features
			{
				FeatureID:    usageNameToID(usage.VipDhcpAllocationUsage),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelTechPreview,
			},
			// Dev-Preview
			{
				FeatureID:    usageNameToID(usage.CPUArchitectureARM64),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
			},
			// Unsupported features
			{
				FeatureID:    usageNameToID(usage.UserManagedNetworkWithVMs),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
		},
	},
}
