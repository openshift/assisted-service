package featuresupport

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

// Interface for support level features that can validate themselves
type SupportLevelFeatureValidator interface {
	// Validate the feature against cluster state and updateParams. Returns descriptive error
	Validate(cluster *common.Cluster, updateParams interface{}) error
}

type SupportLevelFeature interface {
	// New - Initialize new SupportLevelFeature structure while setting its default attributes
	New() SupportLevelFeature
	// getId - Get SupportLevelFeature unique ID
	getId() models.FeatureSupportLevelID
	// GetName - Get SupportLevelFeature user friendly name
	GetName() string
	// getSupportLevel - Get feature support-level value, filtered by given filters (e.g. OpenshiftVersion, CpuArchitecture)
	getSupportLevel(filters SupportLevelFilters) models.SupportLevel
	// getIncompatibleFeatures - Get a list of features that cannot exist alongside this feature
	getIncompatibleFeatures(openshiftVersion string) *[]models.FeatureSupportLevelID
	// getIncompatibleArchitectures - Get a list of architectures which the given feature will not work on
	getIncompatibleArchitectures(openshiftVersion *string) *[]models.ArchitectureSupportLevelID
	// getFeatureActiveLevel - Get the feature status, if it's active, not-active or not relevant (in cases where there is no meaning for that feature to be active)
	getFeatureActiveLevel(cluster *common.Cluster, infraEnv *models.InfraEnv, clusterUpdateParams *models.V2ClusterUpdateParams, infraenvUpdateParams *models.InfraEnvUpdateParams) featureActiveLevel
}

type SupportLevelFilters struct {
	OpenshiftVersion     string
	CPUArchitecture      *string
	PlatformType         *models.PlatformType
	ExternalPlatformName *string
}

type featureActiveLevel string

const (
	activeLevelActive     featureActiveLevel = "Active"
	activeLevelNotActive  featureActiveLevel = "NotActive"
	activeLeveNotRelevant featureActiveLevel = "NotRelevant"
)
