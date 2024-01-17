package releasesources

import (
	"fmt"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func NewReleaseSourcesHandler(releaseSources models.ReleaseSources, logger logrus.FieldLogger, db *gorm.DB, config Config) releaseSourcesHandler {
	return releaseSourcesHandler{
		releaseSources:     releaseSources,
		log:                logger,
		db:                 db,
		config:             config,
		releasesClient:     OpenShiftReleasesAPIClient{BaseUrl: config.OpenshiftReleasesAPIBaseUrl},
		supportLevelClient: OpenShiftSupportLevelAPIClient{BaseUrl: config.OpenshiftSupportLevelAPIBaseUrl},
	}
}

type releaseSourcesHandler struct {
	releaseSources     models.ReleaseSources
	log                logrus.FieldLogger
	db                 *gorm.DB
	config             Config
	releasesClient     OpenShiftReleasesAPIClientInterface
	supportLevelClient OpenShiftSupportLevelAPIClientInterface
}

func (h *releaseSourcesHandler) getReleaseImageReference(version, cpuArchitecture string) string {
	return fmt.Sprintf(common.ReleaseImageReferenceTemplateSaaS, version, cpuArchitecture)
}

func (h *releaseSourcesHandler) addReleaseImage(releases []*common.ReleaseImage, channel, version, cpuArchitecture string) ([]*common.ReleaseImage, error) {
	majorMinorVersion, err := common.GetMajorMinorVersion(version)
	if err != nil {
		return nil, err
	}

	newOpenshiftVersion := majorMinorVersion
	newVersion := &version
	if cpuArchitecture == common.MultiCPUArchitecture {
		newOpenshiftVersion = swag.String(fmt.Sprintf("%s-%s", *newOpenshiftVersion, common.MultiCPUArchitecture))
		newVersion = swag.String(fmt.Sprintf("%s-%s", *newVersion, common.MultiCPUArchitecture))
	}

	newRelease := common.ReleaseImage{
		ReleaseImage: models.ReleaseImage{
			OpenshiftVersion: newOpenshiftVersion,
			CPUArchitecture:  &cpuArchitecture,
			Version:          newVersion,
			URL:              swag.String(h.getReleaseImageReference(version, cpuArchitecture)),
			Default:          false,
		},
		Channel: channel,
	}

	releases = append(releases, &newRelease)

	return releases, nil
}

func (h *releaseSourcesHandler) insertToDB(tx *gorm.DB, releases []*common.ReleaseImage) error {
	h.log.Debugf("Inserting %d records to openshift_versions table", len(releases))

	if len(releases) == 0 {
		return nil
	}

	return tx.Create(releases).Error
}

// removeReleaseImageDuplicateRecords removes duplcates from release images slice
func (h *releaseSourcesHandler) removeReleaseImageDuplicateRecords(releases []*common.ReleaseImage) []*common.ReleaseImage {
	resultReleases := []*common.ReleaseImage{}
	releaseImageReferences := map[string]bool{}

	for _, releaseImage := range releases {
		if releaseImageReferences[*releaseImage.URL] {
			continue
		}

		releaseImageReferences[*releaseImage.URL] = true
		resultReleases = append(resultReleases, releaseImage)
	}

	return resultReleases
}

func (h *releaseSourcesHandler) removeReleaseImageDuplicates(releases []*common.ReleaseImage) []*common.ReleaseImage {
	releases = h.removeReleaseImageDuplicatesFromDifferentChannels(releases)
	releases = h.removeReleaseImageDuplicateRecords(releases)
	return releases
}

// getReleasesWithoutDuplicates removes duplicate release images from different channels
func (h *releaseSourcesHandler) removeReleaseImageDuplicatesFromDifferentChannels(releases []*common.ReleaseImage) []*common.ReleaseImage {
	resultReleases := []*common.ReleaseImage{}
	stableReleasesReferences := map[string]bool{}

	for _, release := range releases {
		if release.Channel == common.OpenshiftReleaseChannelStable {
			stableReleasesReferences[*release.URL] = true
		}
	}

	// Add only releases which does not have a stable duplicate
	for _, release := range releases {
		if release.Channel != common.OpenshiftReleaseChannelStable && stableReleasesReferences[*release.URL] {
			continue
		}

		resultReleases = append(resultReleases, release)
	}

	return resultReleases
}

func (h *releaseSourcesHandler) getBaseReleases() ([]*common.ReleaseImage, error) {
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

	releases := []*common.ReleaseImage{}
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

				graph, err := h.releasesClient.GetReleases(channel, openshiftVersion, cpuArchitecture)
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
					if strings.HasPrefix(node.Version, openshiftVersion) {
						// Got a match for OpenShiftReleaseImage
						releases, err = h.addReleaseImage(releases, channel, node.Version, cpuArchitecture)
						if err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}

	// remove duplicates

	h.log.Debugf("Got %d releases", len(releases))

	return releases, nil
}

func (h *releaseSourcesHandler) syncReleaseImages(tx *gorm.DB) error {
	releases, err := h.getBaseReleases()
	if err != nil {
		return err
	}

	// If no releases found, we prefer not to delete the current ones in the table,
	// and the rest of the operations are redundant
	if len(releases) == 0 {
		return nil
	}

	releases = h.removeReleaseImageDuplicates(releases)
	err = h.setDefaultRelease(releases)
	if err != nil {
		return err
	}

	err = h.setSupportLevels(releases, tx)
	if err != nil {
		return err
	}

	// Deleting all releases before adding again in order to
	// store only the releases according to RELEASE_SOURCES
	err = h.deleteAllReleases(tx)
	if err != nil {
		return err
	}

	err = h.insertToDB(tx, releases)
	if err != nil {
		return err
	}

	return nil
}

func (h *releaseSourcesHandler) createSupportLevels(supportLevelGraph *SupportLevelGraph) []*common.OpenshiftVersionSupportLevel {
	mapAPISupportLevelToOurSupportLevel := map[string]string{
		"End of life":         models.OpenshiftVersionSupportLevelEndOfLife,
		"Maintenance Support": models.OpenshiftVersionSupportLevelMaintenance,
		"Full Support":        models.OpenshiftVersionSupportLevelProduction,
	}

	supportLevels := []*common.OpenshiftVersionSupportLevel{}

	for _, Data := range supportLevelGraph.Data {
		for _, version := range Data.Versions {
			if convertion, ok := mapAPISupportLevelToOurSupportLevel[version.Type]; ok {
				version.Type = convertion
			}
			supportLevels = append(
				supportLevels,
				&common.OpenshiftVersionSupportLevel{OpenshiftVersion: version.Name, SupportLevel: version.Type},
			)
		}
	}

	return supportLevels
}

// All candidate/pre-release releases are considered support level 'beta'.
// The rest are stable releases, and their support level is set according to the mapping
// retrieved from the API.
func (h *releaseSourcesHandler) setSupportLevels(releases []*common.ReleaseImage, tx *gorm.DB) error {
	supportLevelGraph, err := h.supportLevelClient.GetSupportLevels(h.config.OpenshiftMajorVersion)
	if err != nil {
		return err
	}

	supportLevels := h.createSupportLevels(supportLevelGraph)
	if err = tx.Where("TRUE").Delete(&common.OpenshiftVersionSupportLevel{}).Error; err != nil {
		h.log.WithError(err).Debug("error occurred while trying to delete suppurt levels table")
		return err
	}

	if err = tx.Create(&supportLevels).Error; err != nil {
		h.log.WithError(err).Debug("error occurred while trying to create suppurt levels table")
		return err
	}

	for _, release := range releases {
		isPreRelease, err := common.IsVersionPreRelease(*release.Version)
		if err != nil {
			return err
		}

		if release.Channel == common.OpenshiftReleaseChannelCandidate || *isPreRelease {
			release.SupportLevel = models.OpenshiftVersionSupportLevelBeta
			continue
		}

		majorMinorVersion := release.OpenshiftVersion
		for _, supportLevel := range supportLevels {
			if supportLevel.OpenshiftVersion == *majorMinorVersion {
				release.SupportLevel = supportLevel.SupportLevel
				break
			}
		}

		if release.SupportLevel == "" {
			return fmt.Errorf("release %s did not appear in the support level mapping and is not a prerelease or candidate", *release.Version)
		}
	}

	return nil
}

// Labels the latest stable release of the default CPU architecture as default
func (h *releaseSourcesHandler) setDefaultRelease(releases []*common.ReleaseImage) error {
	var latestStableRelease *common.ReleaseImage

	for _, release := range releases {
		if *release.CPUArchitecture != common.DefaultCPUArchitecture || release.Channel != common.OpenshiftReleaseChannelStable {
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
		h.log.Debugf("No stable releases of CPU architecture %s found", common.DefaultCPUArchitecture)
		return nil
	}

	h.log.Debugf("Labeling release %s with CPU architecture %s as default", *latestStableRelease.Version, common.DefaultCPUArchitecture)
	latestStableRelease.Default = true

	return nil
}

func (h *releaseSourcesHandler) deleteAllReleases(tx *gorm.DB) error {
	var count int64
	err := tx.Model(&common.ReleaseImage{}).Count(&count).Error
	if err != nil {
		return err
	}
	h.log.Debugf("Deleting all openshift_versions table records. %d records", count)
	return tx.Where("TRUE").Delete(&common.ReleaseImage{}).Error
}

func (h *releaseSourcesHandler) syncReleaseImagesWithErr(tx *gorm.DB) error {
	err := h.validateReleaseSources()
	if err != nil {
		return err
	}

	err = h.syncReleaseImages(tx)
	if err != nil {
		return err
	}

	return tx.Commit().Error

}

func (h *releaseSourcesHandler) SyncReleaseImages() {
	h.log.Debug("Starting SQL transaction")
	tx := h.db.Begin()

	err := h.syncReleaseImagesWithErr(tx)
	if err != nil {
		h.log.WithError(err).Warn("Failed to sync OpenShift releases, rolling back")
		tx.Rollback()
	}
}

func (h *releaseSourcesHandler) validateReleaseSources() error {
	err := h.releaseSources.Validate(strfmt.Default)
	if err != nil {
		return err
	}

	for _, releaseSource := range h.releaseSources {
		openshiftVersion := *releaseSource.OpenshiftVersion
		_, err = version.NewVersion(openshiftVersion)
		if err != nil {
			h.log.Debugf("Failed to create a version struct from %s", openshiftVersion)
			return err
		}
	}

	return nil
}
