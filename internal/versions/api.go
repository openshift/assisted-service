package versions

import (
	context "context"
	"net/http"
	"strings"

	middleware "github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	models "github.com/openshift/assisted-service/models"
	auth "github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/versions"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

type NewVersionsAPIHandlerParams struct {
	AuthzHandler    auth.Authorizer
	Versions        Versions
	Log             logrus.FieldLogger
	VersionsHandler *handler
	OSImages        OSImages
}

type Versions struct {
	SelfVersion     string `envconfig:"SELF_VERSION" default:"Unknown"`
	AgentDockerImg  string `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer-agent:latest"`
	InstallerImage  string `envconfig:"INSTALLER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer:latest"`
	ControllerImage string `envconfig:"CONTROLLER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer-controller:latest"`
	ReleaseTag      string `envconfig:"RELEASE_TAG" default:""`
}

type apiHandler struct {
	authzHandler    auth.Authorizer
	versions        Versions
	log             logrus.FieldLogger
	versionsHandler *handler
	osImages        OSImages
}

var _ restapi.VersionsAPI = (*apiHandler)(nil)

func NewAPIHandler(params NewVersionsAPIHandlerParams) restapi.VersionsAPI {
	return &apiHandler{
		authzHandler:    params.AuthzHandler,
		versions:        params.Versions,
		log:             params.Log,
		versionsHandler: params.VersionsHandler,
		osImages:        params.OSImages,
	}
}

func (h *apiHandler) V2ListComponentVersions(ctx context.Context, params operations.V2ListComponentVersionsParams) middleware.Responder {
	return operations.NewV2ListComponentVersionsOK().
		WithPayload(GetListVersionsFromVersions(h.versions))
}

func GetListVersionsFromVersions(v Versions) *models.ListVersions {
	return &models.ListVersions{
		Versions:   GetModelVersions(v),
		ReleaseTag: v.ReleaseTag,
	}
}

func GetModelVersions(v Versions) models.Versions {
	return models.Versions{
		"assisted-installer-service":    v.SelfVersion,
		"discovery-agent":               v.AgentDockerImg,
		"assisted-installer":            v.InstallerImage,
		"assisted-installer-controller": v.ControllerImage,
	}
}

// filterIgnoredReleases filteres out releases we don't want to consider,
// which is represented in a configuration
func (h *apiHandler) filterIgnoredReleases(releases models.OpenshiftVersions) models.OpenshiftVersions {
	for release := range releases {
		for _, ignoredRelease := range h.versionsHandler.ignoredReleaseImages {
			if strings.Contains(release, ignoredRelease) {
				delete(releases, release)
			}
		}
	}

	return releases
}

func (h *apiHandler) getOpenshiftMajorMinorVersionsFromReleaseSources() []string {
	versions := []string{}
	for _, releaseSource := range h.versionsHandler.releaseSources {
		versions = append(versions, *releaseSource.OpenshiftVersion)
	}

	return versions
}

func (h *apiHandler) getReleaseImagesForOnlyLatestParamFromDB(onlyLatest *bool) (models.ReleaseImages, error) {
	var (
		err                 error
		versions            []string
		releaseImages       models.ReleaseImages
		latestReleaseImages models.ReleaseImages
		latestRelease       *models.ReleaseImage
	)

	if swag.BoolValue(onlyLatest) {
		versions = h.getOpenshiftMajorMinorVersionsFromReleaseSources()
		for _, majorMinorVersion := range versions {
			releaseImages, err = h.versionsHandler.getReleaseImagesByPartVersionFromDB(majorMinorVersion)
			if err != nil {
				return nil, err
			}

			latestRelease, err = h.versionsHandler.getLatestDBReleaseImage(releaseImages)
			if err != nil {
				return nil, err
			}

			if latestRelease != nil {
				latestReleaseImages = append(latestReleaseImages, latestRelease)
			}
		}

		return latestReleaseImages, nil
	}

	releaseImages, err = common.GetReleaseImagesFromDBWhere(h.versionsHandler.db)
	if err != nil {
		h.log.WithError(err).Debug("error occurred while trying to get release images from the DB")
		return nil, err
	}

	return releaseImages, nil
}

func (h *apiHandler) getOpenshiftReleasesForVersionPatternParamFromDB(versionPattern *string) (models.ReleaseImages, error) {
	var (
		releaseImages models.ReleaseImages
		err           error
	)

	if versionPattern == nil {
		releaseImages, err = common.GetReleaseImagesFromDBWhere(h.versionsHandler.db)
		if err != nil {
			h.log.WithError(err).Debug("error occurred while trying to get openshift versions from the DB")
			return nil, err
		}

		return releaseImages, nil
	}

	releaseImages, err = common.GetReleaseImagesFromDBWhere(
		h.versionsHandler.db,
		"version LIKE ?", "%"+h.versionsHandler.escapeWildcardCharacters(*versionPattern)+"%",
	)
	if err != nil {
		h.log.WithError(err).Debugf("error occurred while trying to get openshift releases for ocp version %s from the DB", *versionPattern)
		return nil, err
	}

	return releaseImages, nil
}

func (h *apiHandler) getCommonReleaseImages(releaseImagesSlice1 models.ReleaseImages, releaseImagesSlice2 models.ReleaseImages) models.ReleaseImages {
	intersectionMap := map[string]bool{}
	intersection := models.ReleaseImages{}

	for _, version := range releaseImagesSlice1 {
		intersectionMap[*version.Version] = true
	}

	for _, version := range releaseImagesSlice2 {
		if intersectionMap[*version.Version] {
			intersection = append(intersection, version)
		}
	}

	return intersection
}

func (h *apiHandler) getOpenshiftVersionsFromDB(onlyLatest *bool, versionPattern *string) (models.ReleaseImages, error) {
	releaseImagesWithOnlyLatestParam, err := h.getReleaseImagesForOnlyLatestParamFromDB(onlyLatest)
	if err != nil {
		h.log.Debugf("error occurred while trying to get openshift versions from the DB with parameter 'only latest' %b", onlyLatest)
		return nil, err
	}

	releaseImagesWithVersionPatternParam, err := h.getOpenshiftReleasesForVersionPatternParamFromDB(versionPattern)
	if err != nil {
		h.log.Debugf("error occurred while trying to get openshift versions from the DB with parameter 'version pattern' %b", versionPattern)
		return nil, err
	}

	return h.getCommonReleaseImages(releaseImagesWithOnlyLatestParam, releaseImagesWithVersionPatternParam), nil
}

// mergeConfigAndDBOpenshiftVersions merges configuration and db OpenshiftVersions such that
// in case of duplicates, configuration releases have precedence
func (h *apiHandler) mergeConfigAndDBOpenshiftVersions(
	configReleaseImages,
	dbReleaseImages models.OpenshiftVersions,
) models.OpenshiftVersions {
	for version, openshiftVersion := range configReleaseImages {
		// If there is a default in the configuration, we use it instead of the db default
		if openshiftVersion.Default {
			for dbOpenshiftVersion, dbOpenshiftVersionModel := range dbReleaseImages {
				dbOpenshiftVersionModel.Default = false
				dbReleaseImages[dbOpenshiftVersion] = dbOpenshiftVersionModel
			}
		}
		dbReleaseImages[version] = openshiftVersion
	}

	return dbReleaseImages
}

func (h *apiHandler) createOpenshiftVersionsFromReleaseImages(releaseImages models.ReleaseImages, hasMultiarchAuthorization bool) models.OpenshiftVersions {
	openshiftVersions := models.OpenshiftVersions{}

	for _, releaseImage := range releaseImages {
		supportedArchs := releaseImage.CPUArchitectures
		// We need to have backwards-compatibility for release images that provide supported
		// architecture only as string and not []string. This code should be unreachable as
		// at this moment we should have already propagated []string in the init handler for
		// Versions, but for safety an additional check is added here.
		if len(supportedArchs) == 0 {
			supportedArchs = []string{*releaseImage.CPUArchitecture}
		}

		// (MGMT-11859) We are filtering out multiarch release images so that they are available
		//              only for customers allowed to use them. This is in order to be able to
		//              expose them in OCP pre-4.13 without making them generally available.
		if len(supportedArchs) > 1 {
			if !hasMultiarchAuthorization {
				continue
			}
		}

		for _, arch := range supportedArchs {
			// In order to handle multi-arch release images correctly in the UI, we need to tune
			// their DisplayName to contain an appropriate suffix. If the suffix comes from the
			// JSON definition we do nothing, but in case it's missing there (because CVO is not
			// reporting it), we add it ourselves.
			displayName := *releaseImage.Version
			if len(supportedArchs) > 1 && !strings.HasSuffix(displayName, "-multi") {
				displayName = displayName + "-multi"
			}

			key := displayName

			if arch == "" {
				// Empty implies default architecture
				arch = common.DefaultCPUArchitecture
			}

			// In order to mark a specific version and architecture as supported we do not
			// only need to have an available release image, but we need RHCOS image as well.
			if _, err := h.osImages.GetOsImage(key, arch); err != nil {
				h.log.Debugf("Marking architecture %s for version %s as not available because no matching OS image found", arch, key)
				continue
			}

			var supportLevel *string
			supportLevel, err := h.versionsHandler.getReleaseSupportLevel(*releaseImage)
			if err != nil {
				h.log.WithError(err).Debugf("error occurred while trying to get support level for release: %s, skipping this release", *releaseImage.Version)
				continue
			}

			openshiftVersion, exists := openshiftVersions[key]
			if !exists {
				openshiftVersion = models.OpenshiftVersion{
					CPUArchitectures: []string{arch},
					Default:          releaseImage.Default,
					DisplayName:      swag.String(displayName),
					SupportLevel:     supportLevel,
				}
				openshiftVersions[key] = openshiftVersion
			} else {
				// For backwards compatibility we handle a scenario when single-arch image exists
				// next to the multi-arch one containing the same architecture. We want to avoid
				// duplicated entry in such a case.
				if !funk.Contains(openshiftVersion.CPUArchitectures, arch) {
					openshiftVersion.CPUArchitectures = append(openshiftVersion.CPUArchitectures, arch)
				}
				openshiftVersion.Default = releaseImage.Default || openshiftVersion.Default
				openshiftVersions[key] = openshiftVersion
			}
		}
	}

	return openshiftVersions
}

func (h *apiHandler) V2ListSupportedOpenshiftVersions(ctx context.Context, params operations.V2ListSupportedOpenshiftVersionsParams) middleware.Responder {
	hasMultiarchAuthorization, err := h.authzHandler.HasOrgBasedCapability(ctx, ocm.MultiarchCapabilityName)
	if err != nil {
		h.log.WithError(err).Errorf("failed to get %s capability", ocm.MultiarchCapabilityName)
		return operations.NewV2ListSupportedOpenshiftVersionsInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if strings.HasSuffix(swag.StringValue(params.VersionPattern), "-multi") && !hasMultiarchAuthorization {
		return operations.NewV2ListSupportedOpenshiftVersionsBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	configurationReleaseImages := h.versionsHandler.releaseImages
	configurationOpenshiftVersions := h.createOpenshiftVersionsFromReleaseImages(configurationReleaseImages, hasMultiarchAuthorization)
	dbReleaseImages, err := h.getOpenshiftVersionsFromDB(params.OnlyLatest, params.VersionPattern)
	if err != nil {
		h.log.WithError(err).Debug("error occurred while trying to get release images from the DB")
		return operations.NewV2ListSupportedOpenshiftVersionsInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	dbOpenshiftVersions := h.createOpenshiftVersionsFromReleaseImages(dbReleaseImages, hasMultiarchAuthorization)
	flteredDBOpenshiftVersions := h.filterIgnoredReleases(dbOpenshiftVersions)
	openshiftVersions := h.mergeConfigAndDBOpenshiftVersions(
		configurationOpenshiftVersions,
		flteredDBOpenshiftVersions,
	)

	return operations.NewV2ListSupportedOpenshiftVersionsOK().WithPayload(openshiftVersions)
}
