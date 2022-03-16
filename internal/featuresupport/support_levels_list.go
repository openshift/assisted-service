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
				FeatureID:    usageNameToID(usage.VipDhcpAllocationUsage),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
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
				FeatureID:    usageNameToID(usage.ClusterManagedNetworkWithVMs),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.8",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Dev-Preview features
			{
				FeatureID:    usageNameToID(usage.VipDhcpAllocationUsage),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
			},
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
				FeatureID:    usageNameToID(usage.ClusterManagedNetworkWithVMs),
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
			// Dev-Preview features
			{
				FeatureID:    usageNameToID(usage.VipDhcpAllocationUsage),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
			},
			// Unsupported features
			{
				FeatureID:    usageNameToID(usage.CPUArchitectureARM64),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
			{
				FeatureID:    usageNameToID(usage.ClusterManagedNetworkWithVMs),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
		},
	},
	&models.FeatureSupportLevel{
		OpenshiftVersion: "4.10",
		Features: []*models.FeatureSupportLevelFeaturesItems0{
			// Supported
			{
				FeatureID:    usageNameToID(usage.HighAvailabilityModeUsage),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelSupported,
			},
			{
				FeatureID:    usageNameToID(usage.VipDhcpAllocationUsage),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
			},
			{
				FeatureID:    usageNameToID(usage.CPUArchitectureARM64),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelDevPreview,
			},
			{
				FeatureID:    usageNameToID(usage.ClusterManagedNetworkWithVMs),
				SupportLevel: models.FeatureSupportLevelFeaturesItems0SupportLevelUnsupported,
			},
		},
	},
}
