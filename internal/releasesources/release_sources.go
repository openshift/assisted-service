package releasesources

import (
	"fmt"
	"net/url"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	goversion "github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/leader"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

const releaseImageReferenceTemplateSaaS string = "quay.io/openshift-release-dev/ocp-release:%s-%s"

type releaseSourcesHandler struct {
	releaseSources              models.ReleaseSources
	enrichedStaticReleaseImages []*enrichedReleaseImage
	log                         logrus.FieldLogger
	db                          *gorm.DB
	config                      Config
	lead                        leader.ElectorInterface
	releasesClient              openShiftReleasesAPIClientInterface
	supportLevelClient          openShiftSupportLevelAPIClientInterface
}

// enrichedReleaseImage is a struct designed to aggregate release images and enrich them by adding release channels
type enrichedReleaseImage struct {
	models.ReleaseImage
	channel models.ReleaseChannel
}

func NewReleaseSourcesHandler(
	releaseSources models.ReleaseSources,
	releaseImages models.ReleaseImages,
	logger logrus.FieldLogger,
	db *gorm.DB,
	config Config,
	lead leader.ElectorInterface,
) (*releaseSourcesHandler, error) {
	// Currently OCM_BASE_URL=https://api.<something>openshift.com such that <something>=(integration.)/(stage.)/(.),
	// which means scheme and host
	ocmBaseURL, err := url.Parse(config.OCMBaseURL)
	if err != nil {
		return nil, errors.Wrapf(err, "error occurred while trying to parse OCM base URL: %s", config.OCMBaseURL)
	}
	ocmBaseURL.Path = OpenshiftUpdateServiceAPIURLPath
	// Currently RED_HAT_PRODUCT_LIFE_CYCLE_DATA_API_BASE_URL=https://access.redhat.com/product-life-cycles/api/v1/products,
	// which means schema, host and path
	redHatCustomerPortalBaseURL, err := url.Parse(config.RedHatProductLifeCycleDataAPIBaseURL)
	if err != nil {
		return nil, errors.Wrapf(err, "error occurred while trying to parse Red Hat Customer Portal base URL: %s", config.RedHatProductLifeCycleDataAPIBaseURL)
	}

	err = validateReleaseSources(releaseSources)
	if err != nil {
		return nil, errors.Wrap(err, "release sources validation failed")
	}

	err = validateStaticReleaseImages(releaseImages)
	if err != nil {
		return nil, errors.Wrap(err, "static release images validation failed")
	}

	enrichedReleaseImages := []*enrichedReleaseImage{}
	for _, releaseImage := range releaseImages {
		enrichedReleaseImages = append(enrichedReleaseImages, &enrichedReleaseImage{ReleaseImage: *releaseImage})
	}

	return &releaseSourcesHandler{
		releaseSources:              releaseSources,
		enrichedStaticReleaseImages: enrichedReleaseImages,
		log:                         logger,
		db:                          db,
		config:                      config,
		lead:                        lead,
		releasesClient: openShiftReleasesAPIClient{
			baseURL: *ocmBaseURL,
		},
		supportLevelClient: openShiftSupportLevelAPIClient{
			baseURL: *redHatCustomerPortalBaseURL,
		},
	}, nil
}

func (h *releaseSourcesHandler) createReleaseImage(
	channel models.ReleaseChannel, majorMinorVersion, majorMinorPatchVersion, cpuArchitecture string,
) (*enrichedReleaseImage, error) {
	cpuArchitectures := []string{cpuArchitecture}
	newReleaseImageReference := getReleaseImageReference(majorMinorPatchVersion, cpuArchitecture)

	if cpuArchitecture == common.MultiCPUArchitecture {
		releaseSourceInterface := funk.Find(h.releaseSources, func(releaseSource *models.ReleaseSource) bool {
			return *releaseSource.OpenshiftVersion == majorMinorVersion
		})
		releaseSource, ok := releaseSourceInterface.(*models.ReleaseSource)
		if !ok {
			return nil, errors.Errorf("error occurred while trying to find release source with openshift version: %s", majorMinorVersion)
		}
		majorMinorVersion = fmt.Sprintf("%s-%s", majorMinorVersion, common.MultiCPUArchitecture)
		majorMinorPatchVersion = fmt.Sprintf("%s-%s", majorMinorPatchVersion, common.MultiCPUArchitecture)
		cpuArchitectures = releaseSource.MultiCPUArchitectures
	}

	return &enrichedReleaseImage{
		ReleaseImage: models.ReleaseImage{
			OpenshiftVersion: &majorMinorVersion,
			CPUArchitecture:  &cpuArchitecture,
			CPUArchitectures: cpuArchitectures,
			Version:          &majorMinorPatchVersion,
			URL:              swag.String(newReleaseImageReference),
			Default:          false,
		},
		channel: channel,
	}, nil
}

func (h *releaseSourcesHandler) insertToDB(tx *gorm.DB, enrichedReleaseImages []*enrichedReleaseImage) error {
	h.log.Debugf("Inserting %d records to openshift_versions table", len(enrichedReleaseImages))

	if len(enrichedReleaseImages) == 0 {
		return nil
	}

	releaseImages := models.ReleaseImages{}
	for _, enrichedReleaseImage := range enrichedReleaseImages {
		releaseImages = append(releaseImages, &enrichedReleaseImage.ReleaseImage)
	}

	return tx.Create(releaseImages).Error
}

// removeReleaseImageDuplicates removes release images such that only one release image
// of each version, CPU architecture will remain
func (h *releaseSourcesHandler) removeReleaseImageDuplicates(releases []*enrichedReleaseImage) ([]*enrichedReleaseImage, error) {
	releaseImagesMap := map[string]*enrichedReleaseImage{}

	for _, releaseImage := range releases {
		duplicateEnrichedReleaseImage, exists := releaseImagesMap[*releaseImage.URL]
		if exists {
			// If the release image that already exists or its diplicate are labeled as default,
			// label the one already considered as default
			if duplicateEnrichedReleaseImage.Default || releaseImage.Default {
				duplicateEnrichedReleaseImage.Default = true
			}

			// If the release image that aleady exists has support level beta (as it came from candidate channel),
			// replace its support level with the other release's support level
			if duplicateEnrichedReleaseImage.SupportLevel == models.OpenshiftVersionSupportLevelBeta {
				duplicateEnrichedReleaseImage.SupportLevel = releaseImage.SupportLevel
			}

			continue
		}

		releaseImagesMap[*releaseImage.URL] = releaseImage
	}

	mappingResult := funk.Map(releaseImagesMap, func(url string, enrichedReleaseImage *enrichedReleaseImage) *enrichedReleaseImage {
		return enrichedReleaseImage
	})
	if enrichedReleaseImages, ok := mappingResult.([]*enrichedReleaseImage); ok {
		return enrichedReleaseImages, nil
	}

	return nil, errors.New("error occurred while trying to convert the map to slice during removal of release image duplicates")

}

func (h *releaseSourcesHandler) getDynamicReleaseImages() ([]*enrichedReleaseImage, error) {
	// OCP releases API needs amd64, arm64 instead of x86_64, aarch64 respectively
	cpuArchMapToAPIArch := map[string]string{
		common.X86CPUArchitecture:     common.AMD64CPUArchitecture,
		common.AARCH64CPUArchitecture: common.ARM64CPUArchitecture,
	}

	// switch back
	cpuArchMapFromAPIArch := map[string]string{
		common.AMD64CPUArchitecture: common.X86CPUArchitecture,
		common.ARM64CPUArchitecture: common.AARCH64CPUArchitecture,
	}

	releases := []*enrichedReleaseImage{}
	// Aggregate slice of releases from releases API
	for _, releaseSource := range h.releaseSources {
		openshiftVersion := *releaseSource.OpenshiftVersion
		for _, upgradeChannel := range releaseSource.UpgradeChannels {
			cpuArchitecture := *upgradeChannel.CPUArchitecture
			for _, channel := range upgradeChannel.Channels {
				apiCpuArchitecture, shouldSwitch := cpuArchMapToAPIArch[cpuArchitecture]
				if shouldSwitch {
					cpuArchitecture = apiCpuArchitecture
				}

				graph, err := h.releasesClient.getReleases(channel, openshiftVersion, cpuArchitecture)
				if err != nil {
					h.log.WithError(err).Warnf("failed to get releases from api with: %s-%s, %s", channel, openshiftVersion, cpuArchitecture)
					continue
				}

				if shouldSwitch {
					if arch, ok := cpuArchMapFromAPIArch[cpuArchitecture]; ok {
						cpuArchitecture = arch
					}
				}

				for _, node := range graph.Nodes {
					majorMinorVersion, err := common.GetMajorMinorVersion(node.Version)
					if err != nil {
						h.log.WithError(err).Warnf("failed to get the major.minor version of: %s", node.Version)
						continue
					}
					// Validate that the dynamic release fetched from the API matches the correct major.minor version,
					// as release images can reside in different channels from different major.minor versions.
					if openshiftVersion == *majorMinorVersion {
						// Got a match for OpenShift ReleaseImage
						newReleaseImage, err := h.createReleaseImage(channel, *majorMinorVersion, node.Version, cpuArchitecture)
						if err != nil {
							return nil, err
						}
						releases = append(releases, newReleaseImage)
					}
				}
			}
		}
	}

	h.log.Debugf("Got %d releases", len(releases))
	return releases, nil
}

// mergeEnrichedReleaseImages merges static and dynamic release images together such that
// static release images have precedence in case of conflicts e.g. which release is default
func (h *releaseSourcesHandler) mergeEnrichedReleaseImages(staticReleaseImages, dynamicReleaseImages []*enrichedReleaseImage) []*enrichedReleaseImage {
	mergedReleaseImages := []*enrichedReleaseImage{}
	staticReleaseImagesReferenceSet := map[string]bool{}
	defaultStaticReleaseExists := false

	for _, staticRelease := range staticReleaseImages {
		mergedReleaseImages = append(mergedReleaseImages, staticRelease)
		staticReleaseImagesReferenceSet[*staticRelease.URL] = true
		if staticRelease.Default {
			defaultStaticReleaseExists = true
		}
	}

	for _, dynamicRelease := range dynamicReleaseImages {
		if staticReleaseImagesReferenceSet[*dynamicRelease.URL] {
			continue
		}
		if dynamicRelease.Default && defaultStaticReleaseExists {
			dynamicRelease.Default = false
		}
		mergedReleaseImages = append(mergedReleaseImages, dynamicRelease)
	}

	return mergedReleaseImages
}

// SyncReleaseImages is an internal function intended to perform synchronization of release images and return any errors encountered.
// This design is due to SyncReleaseImagesThreadFunc being restricted to thread package functionality and not directly handling errors.
func (h *releaseSourcesHandler) SyncReleaseImages() error {
	if !h.lead.IsLeader() {
		h.log.Debug("Not a leader, exiting SyncReleaseImagesThreadFunc")
		return nil
	}

	enrichedDynamicReleaseImages, err := h.getDynamicReleaseImages()
	if err != nil {
		return err
	}
	h.log.Debugf("Found %d dynamic release Images", len(enrichedDynamicReleaseImages))
	supportLevels, err := h.getSupportLevels()
	if err != nil {
		return err
	}
	err = h.setSupportLevels(enrichedDynamicReleaseImages, supportLevels)
	if err != nil {
		return err
	}
	h.log.Debugf("Found %d static release Images", len(h.enrichedStaticReleaseImages))
	err = h.setSupportLevels(h.enrichedStaticReleaseImages, supportLevels)
	if err != nil {
		return err
	}
	err = h.setDefaultReleaseImage(enrichedDynamicReleaseImages)
	if err != nil {
		return err
	}

	enrichedDynamicReleaseImages, err = h.removeReleaseImageDuplicates(enrichedDynamicReleaseImages)
	if err != nil {
		return err
	}

	EnrichedReleaseImages := h.mergeEnrichedReleaseImages(h.enrichedStaticReleaseImages, enrichedDynamicReleaseImages)

	h.log.Debug("Starting SQL transaction")
	tx := h.db.Begin()
	// Deleting all releases before adding again in order to
	// store only the releases according to RELEASE_SOURCES
	err = h.deleteAllReleases(tx)
	if err != nil {
		tx.Rollback()
		return err
	}

	err = h.insertToDB(tx, EnrichedReleaseImages)
	if err != nil {
		tx.Rollback()
		return err
	}

	h.log.Debug("Commiting changes")
	return tx.Commit().Error
}

// getReleaseImagesMajorOCPVersions retrieves the major OCP versions for both static and dynamic release images discovered.
func (h *releaseSourcesHandler) getReleaseImagesMajorOCPVersions() (ocpMajorVersionSet, error) {
	majorVersions := ocpMajorVersionSet{}

	// First from static release images
	for _, releaseImage := range h.enrichedStaticReleaseImages {
		majorVersion, err := common.GetMajorVersion(*releaseImage.OpenshiftVersion)
		if err != nil {
			return nil, errors.Errorf("error occurred while trying to get the major version of %s", *releaseImage.OpenshiftVersion)
		}
		majorVersions[*majorVersion] = true
	}

	// Then from dynamic release images
	for _, releaseSource := range h.releaseSources {
		majorVersion, err := common.GetMajorVersion(*releaseSource.OpenshiftVersion)
		if err != nil {
			return nil, errors.Errorf("error occurred while trying to get the major version of %s", *releaseSource.OpenshiftVersion)
		}
		majorVersions[*majorVersion] = true
	}

	return majorVersions, nil
}

// getSupportLevels retrieves a mapping from OCP major.minor versions
// to their corresponding support levels for both static and dynamic release images.
func (h *releaseSourcesHandler) getSupportLevels() (ocpVersionSupportLevels, error) {
	ocpMajorVersionSet, err := h.getReleaseImagesMajorOCPVersions()
	if err != nil {
		return nil, err
	}

	supportLevels := ocpVersionSupportLevels{}

	for majorVersion := range ocpMajorVersionSet {
		supportLevelsForMajor, err := h.supportLevelClient.getSupportLevels(majorVersion)
		if err != nil {
			return nil, err
		}

		for majorMinorVersion, supportLevel := range supportLevelsForMajor {
			supportLevels[majorMinorVersion] = supportLevel
		}
	}

	return supportLevels, nil
}

// setSupportLevels sets support level for given release images as follows:
// candidate/pre-release releases are considered support level 'beta'.
// The rest are stable releases, and their support level is set according to the mapping
// retrieved from the API.
func (h *releaseSourcesHandler) setSupportLevels(releases []*enrichedReleaseImage, supportLevels ocpVersionSupportLevels) error {
	for _, release := range releases {
		isPreRelease, err := common.IsVersionPreRelease(*release.Version)
		if err != nil {
			return err
		}

		if release.channel == models.ReleaseChannelCandidate || *isPreRelease {
			release.SupportLevel = models.OpenshiftVersionSupportLevelBeta
			continue
		}

		majorMinorVersion, err := common.GetMajorMinorVersion(*release.Version)
		if err != nil {
			return err
		}

		if supportLevel, exists := supportLevels[*majorMinorVersion]; exists {
			release.SupportLevel = supportLevel
			continue
		}

		return fmt.Errorf("release %s did not appear in the support level mapping and is not a prerelease or candidate", *release.Version)
	}

	return nil
}

// setDefaultReleaseImage labels the latest production release image of the default CPU architecture as default
func (h *releaseSourcesHandler) setDefaultReleaseImage(releases []*enrichedReleaseImage) error {
	var latestStableRelease *enrichedReleaseImage

	for _, release := range releases {
		if *release.CPUArchitecture != common.DefaultCPUArchitecture || release.SupportLevel != models.OpenshiftVersionSupportLevelProduction {
			continue
		}

		if latestStableRelease == nil {
			latestStableRelease = release
		}

		lessThan, err := common.BaseVersionLessThan(*release.Version, *latestStableRelease.Version)
		if err != nil {
			return err
		}

		if lessThan {
			latestStableRelease = release
		}
	}

	if latestStableRelease == nil {
		h.log.Debugf("No production release images of CPU architecture %s found", common.DefaultCPUArchitecture)
		return nil
	}

	h.log.Debugf("Labeling release %s with CPU architecture %s as default", *latestStableRelease.Version, common.DefaultCPUArchitecture)
	latestStableRelease.Default = true

	return nil
}

func (h *releaseSourcesHandler) deleteAllReleases(tx *gorm.DB) error {
	var count int64
	err := tx.Model(&models.ReleaseImage{}).Count(&count).Error
	if err != nil {
		return err
	}
	h.log.Debugf("Truncating all release_images table records. %d records", count)
	return tx.Exec("TRUNCATE TABLE release_images").Error
}

func (h *releaseSourcesHandler) SyncReleaseImagesThreadFunc() {
	err := h.SyncReleaseImages()
	if err != nil {
		h.log.WithError(err).Warn("Failed to sync OpenShift ReleaseImages")
	}
}

func validateReleaseSources(releaseSources models.ReleaseSources) error {
	if releaseSources == nil {
		return nil
	}

	err := releaseSources.Validate(strfmt.Default)
	if err != nil {
		return err
	}

	for _, releaseSource := range releaseSources {
		openshiftVersion := *releaseSource.OpenshiftVersion
		_, err = goversion.NewVersion(openshiftVersion)
		if err != nil {
			return errors.Wrapf(err, "Failed to create a version struct from %s", openshiftVersion)
		}
	}

	return nil
}

func validateStaticReleaseImages(staticReleaseImages models.ReleaseImages) error {
	if staticReleaseImages == nil {
		return nil
	}

	err := staticReleaseImages.Validate(strfmt.Default)
	if err != nil {
		return err
	}

	for _, releaseImage := range staticReleaseImages {
		_, err = goversion.NewVersion(*releaseImage.OpenshiftVersion)
		if err != nil {
			return errors.Wrapf(err, "Failed to create a version struct from %s", *releaseImage.OpenshiftVersion)
		}
		_, err = goversion.NewVersion(*releaseImage.Version)
		if err != nil {
			return errors.Wrapf(err, "Failed to create a version struct from %s", *releaseImage.Version)
		}
	}

	return nil
}

func getReleaseImageReference(version, cpuArchitecture string) string {
	if cpuArchitecture == common.ARM64CPUArchitecture {
		cpuArchitecture = common.AARCH64CPUArchitecture
	}
	return fmt.Sprintf(releaseImageReferenceTemplateSaaS, version, cpuArchitecture)
}
