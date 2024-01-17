package versions

import (
	context "context"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"golang.org/x/sync/semaphore"
	"gorm.io/gorm"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const releaseImageURLPrefix string = "quay.io/openshift-release-dev/ocp-release:"

var supportedMultiArchitectures []string = []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.S390xCPUArchitecture, common.PowerCPUArchitecture}

const majorMinorReleaseSegmentsLength int = 2
const majorMinorPatchReleaseSegmentsLength int = 3

type MustGatherVersion map[string]string
type MustGatherVersions map[string]MustGatherVersion

type NewVersionHandlerParams struct {
	MustGatherVersions   MustGatherVersions
	ReleaseImages        models.ReleaseImages
	ReleaseHandler       oc.Release
	ReleaseImageMirror   string
	Log                  logrus.FieldLogger
	KubeClient           client.Client
	DB                   *gorm.DB
	IgnoredReleaseImages []string
	ReleaseSources       models.ReleaseSources
}

//go:generate mockgen --build_flags=--mod=mod -package versions -destination mock_versions.go -self_package github.com/openshift/assisted-service/internal/versions . Handler
type Handler interface {
	GetReleaseImage(ctx context.Context, openshiftVersion, cpuArchitecture, pullSecret string) (*models.ReleaseImage, error)
	GetDefaultReleaseImageByCPUArchitecture(cpuArchitecture string) (*models.ReleaseImage, error)
	GetReleaseImageByURL(ctx context.Context, url, pullSecret string) (*models.ReleaseImage, error)
	GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error)
	ValidateReleaseImageForRHCOS(rhcosVersion, cpuArch string) error
}

func NewHandler(params NewVersionHandlerParams) (*handler, error) {
	h := &handler{
		mustGatherVersions:   params.MustGatherVersions,
		releaseImages:        params.ReleaseImages,
		releaseHandler:       params.ReleaseHandler,
		releaseImageMirror:   params.ReleaseImageMirror,
		log:                  params.Log,
		kubeClient:           params.KubeClient,
		sem:                  semaphore.NewWeighted(30),
		db:                   params.DB,
		ignoredReleaseImages: params.IgnoredReleaseImages,
		releaseSources:       params.ReleaseSources,
	}

	if err := h.validateVersions(); err != nil {
		return nil, err
	}

	return h, nil
}

type handler struct {
	mustGatherVersions   MustGatherVersions
	releaseImages        models.ReleaseImages
	imagesLock           sync.Mutex
	sem                  *semaphore.Weighted
	releaseHandler       oc.Release
	releaseImageMirror   string
	log                  logrus.FieldLogger
	kubeClient           client.Client
	db                   *gorm.DB
	ignoredReleaseImages []string
	releaseSources       models.ReleaseSources
}

// GetMustGatherImages attempts to get OCP must gather images from the configuration,
// for a given openshift version and cpu architecture. If not found,
// it attempts to find the matching release image in the configuration, get the matching OCP must gather image,
// and add it to the configuration.
func (h *handler) GetMustGatherImages(
	openshiftVersion,
	cpuArchitecture,
	pullSecret string,
) (MustGatherVersion, error) {
	majMinorVersion, err := common.GetMajorMinorVersion(openshiftVersion)
	if err != nil {
		return nil, err
	}

	configKey := fmt.Sprintf("%s-%s", *majMinorVersion, cpuArchitecture)

	if h.mustGatherVersions == nil {
		h.mustGatherVersions = make(MustGatherVersions)
	}
	if h.mustGatherVersions[configKey] == nil {
		h.mustGatherVersions[configKey] = make(MustGatherVersion)
	}

	//check if ocp must-gather image is already in the configuration
	if h.mustGatherVersions[configKey]["ocp"] != "" {
		versions := h.mustGatherVersions[configKey]
		return versions, nil
	}
	//if not, fetch it from the release image and add it to the configuration
	releaseImage, err := h.GetReleaseImage(context.Background(), openshiftVersion, cpuArchitecture, pullSecret)
	if err != nil {
		return nil, err
	}
	ocpMustGatherImage, err := h.releaseHandler.GetMustGatherImage(h.log, *releaseImage.URL, h.releaseImageMirror, pullSecret)
	if err != nil {
		return nil, err
	}
	h.mustGatherVersions[configKey]["ocp"] = ocpMustGatherImage

	versions := h.mustGatherVersions[configKey]
	return versions, nil
}

// Returns the default ReleaseImage entity for a specified CPU architecture
func (h *handler) GetDefaultReleaseImageByCPUArchitecture(cpuArchitecture string) (*models.ReleaseImage, error) {
	if err := h.validateReleaseImageCPUArchitecture(cpuArchitecture); err != nil {
		return nil, err
	}

	// First, try from configuration releases
	defaultReleaseImage := funk.Find(h.releaseImages, func(releaseImage *models.ReleaseImage) bool {
		return releaseImage.Default && *releaseImage.CPUArchitecture == cpuArchitecture
	})

	if defaultReleaseImage != nil {
		return defaultReleaseImage.(*models.ReleaseImage), nil
	}

	// If not found, try from DB releases
	releaseImage, err := common.GetReleaseImageFromDBWhere(
		h.db,
		`"default" = ? AND cpu_architecture = ?`,
		true,
		cpuArchitecture,
	)
	if err != nil {
		h.log.WithError(err).Debug("error occurred while trying to find the default release in the DB")
	}

	if releaseImage != nil {
		return releaseImage, nil
	}

	return nil, errors.Errorf("Default release image is not available")
}

func (h *handler) getReleaseSupportLevel(release models.ReleaseImage) (*string, error) {
	if release.SupportLevel != "" {
		return &release.SupportLevel, nil
	}

	version := strings.TrimSuffix(*release.Version, common.MultiCPUArchitecture)

	isPreRelease, err := common.IsVersionPreRelease(version)
	if err != nil {
		return nil, err
	}

	if *isPreRelease {
		return swag.String(models.OpenshiftVersionSupportLevelBeta), nil
	}

	majorMinorVersion, err := common.GetMajorMinorVersion(version)
	if err != nil {
		return nil, err
	}

	supportLevels, err := common.GetOpenshiftVersionsSupportLevelsFromDBWhere(h.db, "openshift_version = ?", majorMinorVersion)
	if err != nil {
		return nil, err
	}

	if len(supportLevels) == 0 {
		return nil, errors.Errorf("error occurred while trying to get the support level of %s from DB", *release.Version)
	}

	return swag.String(supportLevels[0].SupportLevel), nil
}

func (h *handler) getReleaseImageFromDB(
	openshiftVersion,
	cpuArchitecture string,
) (*models.ReleaseImage, error) {
	openshiftVersion = strings.TrimSuffix(openshiftVersion, "-multi")

	var (
		releaseImage *models.ReleaseImage
		err          error
	)

	versionSegmentsLength, err := common.GetVersionSegmentsLength(openshiftVersion)
	if err != nil {
		h.log.WithError(err).
			Debugf("error uccurred while trying to get the number of segments of version: %s", openshiftVersion)
		return nil, err
	}

	// specific version
	if *versionSegmentsLength >= majorMinorPatchReleaseSegmentsLength {
		releaseImage, err = common.GetReleaseImageFromDBWhere(
			h.db, "version = ? AND cpu_architecture = ?", openshiftVersion, cpuArchitecture,
		)
		if err != nil {
			h.log.WithError(err).
				Debugf(
					"error occurred while trying to get openshift release image for version %s and CPU architecture %s from the DB",
					openshiftVersion,
					cpuArchitecture,
				)
			return nil, err
		}

		return releaseImage, nil
	}

	// get latest release for major.minor or major
	releaseImages, err := h.getReleaseImagesByPartVersionAndArchitectureFromDB(openshiftVersion, cpuArchitecture)
	if err != nil {
		return nil, err
	}

	latestReleaseImage, err := h.getLatestDBReleaseImage(releaseImages)
	if err != nil {
		return nil, err
	}

	return latestReleaseImage, nil
}

func (h *handler) getLatestDBReleaseImage(releases models.ReleaseImages) (*models.ReleaseImage, error) {
	var latestRelease *models.ReleaseImage
	for _, release := range releases {
		if latestRelease == nil {
			latestRelease = release
		}

		greaterThanOrEqual, err := common.VersionGreaterOrEqual(*release.Version, *latestRelease.Version)
		if err != nil {
			h.log.WithError(err).Debugf("error occurred while comparing ocp versions: %s, %s", *release.Version, *latestRelease.Version)
			return nil, err
		}

		if greaterThanOrEqual {
			latestRelease = release
		}
	}

	return latestRelease, nil
}

func (h *handler) getReleaseImagesByPartVersionAndArchitectureFromDB(versionSegments, cpuArchitecture string) (models.ReleaseImages, error) {
	dbReleaseImages, err := common.GetReleaseImagesFromDBWhere(
		h.db,
		"version LIKE ? AND cpu_architecture = ?",
		h.escapeWildcardCharacters(versionSegments)+"."+"%",
		cpuArchitecture,
	)
	if err != nil {
		return nil,
			errors.Errorf("failed to get all versions matching %s and cpu architecture %s from DB", versionSegments, cpuArchitecture)
	}

	return dbReleaseImages, nil
}

func (h *handler) getReleaseImagesByPartVersionFromDB(partVersion string) (models.ReleaseImages, error) {
	dbReleaseImages, err := common.GetReleaseImagesFromDBWhere(h.db,
		"version LIKE ?",
		h.escapeWildcardCharacters(partVersion)+"."+"%",
	)
	if err != nil {
		return nil, errors.Errorf("failed to get all release images matching %s from DB", partVersion)
	}

	return dbReleaseImages, nil
}

// addClusterImageSetReleaseImagesToConfiguration adds all ClusterImageSets it finds to
// the current configuration, if they are absent
func (h *handler) addClusterImageSetReleaseImagesToConfiguration(ctx context.Context, pullSecret string) error {
	clusterImageSets := &hivev1.ClusterImageSetList{}
	if err := h.kubeClient.List(ctx, clusterImageSets); err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, clusterImageSet := range clusterImageSets.Items {
		if err := h.sem.Acquire(ctx, 1); err != nil {
			// don't fail the entire function if this iteration fails to acquire the semaphore
			continue
		}
		wg.Add(1)
		go func(clusterImageSet hivev1.ClusterImageSet) {
			defer func() {
				wg.Done()
				h.sem.Release(1)
			}()
			existsInConfiguration := false
			for _, releaseImage := range h.releaseImages {
				if releaseImage.URL != nil && *releaseImage.URL == clusterImageSet.Spec.ReleaseImage {
					existsInConfiguration = true
					break
				}
			}
			if !existsInConfiguration {
				_, err := h.addReleaseImage(clusterImageSet.Spec.ReleaseImage, pullSecret)
				if err != nil {
					h.log.WithError(err).Warnf("Failed to add release image %s", clusterImageSet.Spec.ReleaseImage)
				}
			}
		}(clusterImageSet)
	}
	wg.Wait()

	return nil
}

func (h *handler) validateReleaseImageVersionAndArchitecture(openshiftVersion, cpuArchitecture string) error {
	if err := common.CheckIfValidVersion(openshiftVersion); err != nil {
		return errors.Errorf("openshift version %s did not pass validation", openshiftVersion)
	}

	if err := h.validateReleaseImageCPUArchitecture(cpuArchitecture); err != nil {
		return err
	}

	return nil
}

func (h *handler) getReleaseImageByVersionSegmentsLength(
	openshiftVersion,
	cpuArchitecture string,
) (*models.ReleaseImage, error) {

	// First, try to get the release image from the configuraion
	image, err := h.getReleaseImageFromConfig(
		openshiftVersion,
		cpuArchitecture,
	)

	if err != nil {
		return nil, err
	}

	if image != nil {
		return image, nil
	}

	// The image doesn't exist in the configuration. Lets try to find it in the db releases
	image, err = h.getReleaseImageFromDB(openshiftVersion, cpuArchitecture)
	if err != nil {
		return nil, err
	}

	if image != nil {
		return image, nil
	}

	return nil, nil
}

func (h *handler) isMultiVersion(openshiftVersion string) bool {
	return strings.HasSuffix(openshiftVersion, "-multi")
}

func (h *handler) getReleaseImageInKubeAPIMode(
	ctx context.Context,
	openshiftVersion,
	cpuArchitecture,
	pullSecret string,
) (*models.ReleaseImage, error) {
	var (
		image *models.ReleaseImage
		err   error
	)
	err = h.addClusterImageSetReleaseImagesToConfiguration(ctx, pullSecret)
	if err != nil {
		return nil, err
	}
	h.log.WithError(err).Debug("error occurred while trying to add cluster image sets to the configuration")

	image, err = h.getReleaseImageFromConfig(
		openshiftVersion,
		cpuArchitecture,
	)
	if err != nil {
		return nil, err
	}
	if image != nil {
		return image, nil
	}

	return nil, nil
}

func (h *handler) getReleaseImageRESTAPIMode(
	openshiftVersion,
	cpuArchitecture string,
) (*models.ReleaseImage, error) {
	var (
		image *models.ReleaseImage
		err   error
	)

	image, err = h.getReleaseImageByVersionSegmentsLength(openshiftVersion, cpuArchitecture)
	if err != nil {
		return nil, err
	}
	if image != nil {
		return image, nil
	}

	// If still no match found and possible, fallback to major.minor and try to find the latest release matching
	versionSegmentsLength, err := common.GetVersionSegmentsLength(openshiftVersion)
	if err != nil {
		h.log.WithError(err).
			Debugf("error uccurred while trying to get the number of segments of version: %s", openshiftVersion)
		return nil, err
	}
	if *versionSegmentsLength < majorMinorReleaseSegmentsLength {
		return nil, nil
	}

	majorMinorVersion, err := common.GetMajorMinorVersion(openshiftVersion)
	if err != nil {
		h.log.WithError(err).Debugf("error occurred while trying to convert %s", openshiftVersion)
		return nil, err
	}

	// Keep the multi suffix if specified
	if h.isMultiVersion(openshiftVersion) {
		majorMinorVersion = swag.String(swag.StringValue(majorMinorVersion) + "-multi")
	}

	image, err = h.getReleaseImageByVersionSegmentsLength(*majorMinorVersion, cpuArchitecture)
	if err != nil {
		return nil, err
	}
	if image != nil {
		return image, nil
	}

	return nil, nil
}

// GetReleaseImage returns the release image that matches the specified openshift version, CPU architecture.
// Whether a release matches is mainly defined by the openshift version argument given:
// If openshift version is given as major.minor.patch with/without pre-release, only exact match will be returned.
// If openshift version is given as major.minor, the latest of this minor will be retrieved
// If openshift version is given as major, the latest of this major will be retrieved
func (h *handler) GetReleaseImage(
	ctx context.Context,
	openshiftVersion,
	cpuArchitecture,
	pullSecret string,
) (*models.ReleaseImage, error) {
	errorMessageTemplate := "The requested release image for version (%s) and CPU architecture (%s) isn't specified in release images list"
	var (
		image *models.ReleaseImage
		err   error
	)

	if err := h.validateReleaseImageVersionAndArchitecture(openshiftVersion, cpuArchitecture); err != nil {
		return nil, err
	}

	// In kubeAPI mode, add clusterImageSets and then attempt to find the release image among them.
	if h.kubeClient != nil {
		image, err = h.getReleaseImageInKubeAPIMode(
			ctx, openshiftVersion, cpuArchitecture, pullSecret,
		)
		if err != nil {
			return nil, err
		}
		if image != nil {
			return image, nil
		}

		return nil, errors.Errorf(
			errorMessageTemplate,
			openshiftVersion, cpuArchitecture)
	}

	// In REST API mode, try to find the release image in the configuration/DB
	image, err = h.getReleaseImageRESTAPIMode(openshiftVersion, cpuArchitecture)
	if err != nil {
		return nil, err
	}
	if image != nil {
		return image, nil
	}

	return nil, errors.Errorf(
		errorMessageTemplate,
		openshiftVersion, cpuArchitecture)
}

// GetReleaseImageByURL attempts to find and return a release from the configuration
// with matching URL. If not found, it adds it to the configuration and returns it.
func (h *handler) GetReleaseImageByURL(ctx context.Context, url, pullSecret string) (*models.ReleaseImage, error) {
	for _, image := range h.releaseImages {
		if swag.StringValue(image.URL) == url {
			return image, nil
		}
	}

	return h.addReleaseImage(url, pullSecret)
}

func (h *handler) doesReleaseMatch(
	release *models.ReleaseImage,
	releaseVersion,
	targetVersion,
	targetArchitecture string,
	targetMultiRelease bool,
) bool {
	candidateMultiRelease := h.isMultiVersion(releaseVersion)
	return candidateMultiRelease == targetMultiRelease &&
		releaseVersion == targetVersion &&
		*release.CPUArchitecture == targetArchitecture
}

func (h *handler) getLatestReleaseMatching(
	openshiftVersion,
	cpuArchitecture string,
	targetMultiRelease bool,
	versionExtractorFunc func(string) (*string, error),
) (*models.ReleaseImage, error) {
	var latestRelease *models.ReleaseImage
	for _, release := range h.releaseImages {
		extractedVersion, err := versionExtractorFunc(*release.Version)
		if err != nil {
			h.log.WithError(err).Debugf("error occurred while trying to extract %s", *release.Version)
			return nil, err
		}

		if targetMultiRelease {
			extractedVersion = swag.String(*extractedVersion + "-multi")
		}

		if h.doesReleaseMatch(release, *extractedVersion, openshiftVersion, cpuArchitecture, targetMultiRelease) {
			// First matching release
			if latestRelease == nil {
				latestRelease = release
				continue
			}

			lessThan, err := common.BaseVersionLessThan(*release.Version, *latestRelease.Version)
			if err != nil {
				h.log.WithError(err).
					Debugf("error occurred while trying to determine if %s is smaller than %s",
						*latestRelease.Version,
						*release.Version,
					)
				return nil, err
			}

			if lessThan {
				latestRelease = release
			}
		}
	}

	return latestRelease, nil
}

// getReleaseImageFromConfig function retrieves a release image from the database that corresponds to the specified OpenShift version and CPU architecture.
// The criteria for a matching release are as follows:
// For an OpenShift version specified as x.y.z, an exact match is required.
// For a version denoted as x.y, any x.y compatible release will be selected, with preference given to the most recent matching version.
// If the OpenShift version includes a 'multi' suffix, the selected release must also be designated as 'multi'.
func (h *handler) getReleaseImageFromConfig(
	openshiftVersion,
	cpuArchitecture string,
) (*models.ReleaseImage, error) {
	if cpuArchitecture == "" {
		// Empty implies default CPU architecture
		cpuArchitecture = common.DefaultCPUArchitecture
	}

	versionSegmentsLength, err := common.GetVersionSegmentsLength(openshiftVersion)
	if err != nil {
		h.log.WithError(err).
			Debugf("error uccurred while trying to get the number of segments of version: %s", openshiftVersion)
		return nil, err
	}

	multiTargetRelease := strings.HasSuffix(openshiftVersion, "-multi")
	if multiTargetRelease {
		cpuArchitecture = common.MultiCPUArchitecture
	}

	// user want exact version
	if *versionSegmentsLength >= majorMinorPatchReleaseSegmentsLength {
		for _, release := range h.releaseImages {
			if *release.Version == *release.Version {
				if h.doesReleaseMatch(release, *release.Version, openshiftVersion, cpuArchitecture, multiTargetRelease) {
					return release, nil
				}
			}
		}

		return nil, nil
	}

	// user wants the latest of major.minor from configuration
	if *versionSegmentsLength == majorMinorReleaseSegmentsLength {
		return h.getLatestReleaseMatching(openshiftVersion, cpuArchitecture, multiTargetRelease, common.GetMajorMinorVersion)
	}

	// User wants latest major version from configuration
	return h.getLatestReleaseMatching(openshiftVersion, cpuArchitecture, multiTargetRelease, common.GetMajorVersion)
}

// ValidateReleaseImageForRHCOS validates whether for a specified RHCOS version we have an OCP
// version that can be used. This functions performs a very weak matching because RHCOS versions
// are very loosely coupled with the OpenShift versions what allows for a variety of mix&match.
func (h *handler) ValidateReleaseImageForRHCOS(rhcosVersion, cpuArchitecture string) error {
	majorMinorRhcosVersion, err := common.GetMajorMinorVersion(rhcosVersion)
	if err != nil {
		return err
	}

	if cpuArchitecture == "" {
		// Empty implies default CPU architecture
		cpuArchitecture = common.DefaultCPUArchitecture
	}

	for _, releaseImage := range h.releaseImages {
		for _, arch := range releaseImage.CPUArchitectures {
			if arch == cpuArchitecture {
				majorMinorVersion, err := common.GetMajorMinorVersion(*releaseImage.OpenshiftVersion)
				if err != nil {
					return err
				}
				if *majorMinorVersion == *majorMinorRhcosVersion {
					h.log.Debugf("Validator for the architecture %s found the following OCP version: %s", cpuArchitecture, *releaseImage.Version)
					return nil
				}
			}
		}
	}

	return errors.Errorf(
		"The requested RHCOS version (%s, arch: %s) does not have a matching OpenShift release image",
		rhcosVersion,
		cpuArchitecture,
	)
}

// addReleaseImage adds verifies that the url is indeed a valid url of a release image,
// and if so, it creates a release image and, adds it to the configuration and returns it
func (h *handler) addReleaseImage(releaseImageUrl, pullSecret string) (*models.ReleaseImage, error) {
	// Get openshift version from release image metadata (oc adm release info)
	ocpReleaseVersion, err := h.releaseHandler.GetOpenshiftVersion(h.log, releaseImageUrl, "", pullSecret)
	if err != nil {
		return nil, err
	}
	h.log.Debugf("For release image %s detected version: %s", releaseImageUrl, ocpReleaseVersion)

	// Get CPU architecture from release image. For single-arch image the returned list will contain
	// as single entry with the architecture. For multi-arch image the list will contain all the architectures
	// that the image references to.
	cpuArchitectures, err := h.releaseHandler.GetReleaseArchitecture(h.log, releaseImageUrl, "", pullSecret)
	if err != nil {
		return nil, err
	}
	h.log.Debugf("For release image %s detected architecture: %s", releaseImageUrl, cpuArchitectures)

	var cpuArchitecture string
	if len(cpuArchitectures) == 1 {
		cpuArchitecture = cpuArchitectures[0]
	} else {
		cpuArchitecture = common.MultiCPUArchitecture
	}

	// lock for the rest of this function so we can call it concurrently
	h.imagesLock.Lock()
	defer h.imagesLock.Unlock()

	// Fetch ReleaseImage if exists (not using GetReleaseImage as we search for the x.y.z image only)
	releaseImage := funk.Find(h.releaseImages, func(releaseImage *models.ReleaseImage) bool {
		return *releaseImage.OpenshiftVersion == ocpReleaseVersion && *releaseImage.CPUArchitecture == cpuArchitecture
	})
	if releaseImage == nil {
		// Create a new ReleaseImage
		releaseImage = &models.ReleaseImage{
			OpenshiftVersion: &ocpReleaseVersion,
			CPUArchitecture:  &cpuArchitecture,
			URL:              &releaseImageUrl,
			Version:          &ocpReleaseVersion,
			CPUArchitectures: cpuArchitectures,
		}

		// Store in releaseImages array
		h.releaseImages = append(h.releaseImages, releaseImage.(*models.ReleaseImage))
		h.log.Infof("Stored release version %s for architecture %s", ocpReleaseVersion, cpuArchitecture)
		if len(cpuArchitectures) > 1 {
			h.log.Infof("Full list or architectures: %s", cpuArchitectures)
		}
	}

	return releaseImage.(*models.ReleaseImage), nil
}

// Ensure no missing values in Release images.
func (h *handler) validateVersions() error {
	// Release images are not mandatory (dynamically added in kube-api flow),
	// validating fields for those specified in list.
	missingValueTemplate := "Missing value in ReleaseImage for '%s' field"
	for _, release := range h.releaseImages {
		if swag.StringValue(release.CPUArchitecture) == "" {
			return errors.Errorf(fmt.Sprintf(missingValueTemplate, "cpu_architecture"))
		}
		if swag.StringValue(release.OpenshiftVersion) == "" {
			return errors.Errorf(fmt.Sprintf(missingValueTemplate, "openshift_version"))
		}
		if swag.StringValue(release.URL) == "" {
			return errors.Errorf(fmt.Sprintf(missingValueTemplate, "url"))
		}
		if swag.StringValue(release.Version) == "" {
			return errors.Errorf(fmt.Sprintf(missingValueTemplate, "version"))
		}
		// Normalize release.CPUArchitecture and release.CPUArchitectures
		// TODO: remove this block when AI starts using aarch64 instead of arm64
		if swag.StringValue(release.CPUArchitecture) == common.MultiCPUArchitecture || swag.StringValue(release.CPUArchitecture) == common.AARCH64CPUArchitecture {
			*release.CPUArchitecture = common.NormalizeCPUArchitecture(*release.CPUArchitecture)
			for i := 0; i < len(release.CPUArchitectures); i++ {
				release.CPUArchitectures[i] = common.NormalizeCPUArchitecture(release.CPUArchitectures[i])
			}

		}
	}

	return nil
}

// GetRevision returns the overall codebase version. It's for detecting
// what code a binary was built from.
func GetRevision() string {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return "<unknown>"
	}

	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return "<unknown>"
}

func extractHost(destination string) string {
	patterns := []string{
		"^\\[([^\\]]+)\\]:\\d+$",
		"^([^:]+[.][^:]+):\\d+$",
	}
	for _, p := range patterns {
		r := regexp.MustCompile(p)
		if matches := r.FindStringSubmatch(destination); len(matches) == 2 {
			return matches[1]
		}
	}
	return destination
}

func GetReleaseImageHost(cluster *common.Cluster, versionHandler Handler) (string, error) {
	releaseImage, err := versionHandler.GetReleaseImage(
		context.Background(),
		cluster.OpenshiftVersion,
		cluster.CPUArchitecture,
		cluster.PullSecret,
	)
	if err != nil {
		return "", err
	}
	splits := strings.Split(swag.StringValue(releaseImage.URL), "/")
	if len(splits) < 2 {
		return "", errors.Errorf("failed to get release image domain from %s", swag.StringValue(releaseImage.URL))
	}
	return extractHost(splits[0]), nil
}

func (h *handler) validateReleaseImageCPUArchitecture(cpuArch string) error {
	switch cpuArch {
	case models.ReleaseImageCPUArchitectureAarch64:
		return nil
	case models.ReleaseImageCPUArchitectureX8664:
		return nil
	case models.ReleaseImageCPUArchitecturePpc64le:
		return nil
	case models.ReleaseImageCPUArchitectureS390x:
		return nil
	case models.ReleaseImageCPUArchitectureMulti:
		return nil
	case models.ReleaseImageCPUArchitectureArm64:
		return nil
	default:
		return errors.Errorf("%s is not a valid release image CPU architecture", cpuArch)
	}
}

func (h *handler) escapeWildcardCharacters(str string) string {
	str = strings.ReplaceAll(str, "\\", "\\\\")
	str = strings.ReplaceAll(str, "_", "\\_")
	str = strings.ReplaceAll(str, "%", "\\%")
	return str
}
