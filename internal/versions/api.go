package versions

import (
	context "context"
	"fmt"
	"strings"

	middleware "github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	models "github.com/openshift/assisted-service/models"
	auth "github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/versions"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

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
	versionsHandler Handler
	osImages        OSImages
	releaseSources  models.ReleaseSources
}

var _ restapi.VersionsAPI = (*apiHandler)(nil)

func NewAPIHandler(
	log logrus.FieldLogger,
	versions Versions,
	authzHandler auth.Authorizer,
	versionHandler Handler,
	osImages OSImages,
	releaseSources models.ReleaseSources,
) restapi.VersionsAPI {
	return &apiHandler{
		authzHandler:    authzHandler,
		versions:        versions,
		log:             log,
		versionsHandler: versionHandler,
		osImages:        osImages,
		releaseSources:  releaseSources,
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

// getLatestReleaseImagesForMajorMinor returns the latest release images (all the CPU architectures) matching a given major.minor version,
// or error if none of the release images match. The latest release image is considered the latest none beta release image,
// or if all matching release images are beta then just the latest.
func getLatestReleaseImagesForMajorMinor(releaseImages models.ReleaseImages, majorMinorVersion string, log logrus.FieldLogger) (models.ReleaseImages, error) {
	var latestReleaseImages models.ReleaseImages
	for _, releaseImage := range releaseImages {
		// Typically, releaseImage.OpenshiftVersion follow the major.minor format, though exceptions exist with static release images.
		// We want to consider only release images matching the given major.minor version.
		isMajorMinorEqual, err := common.BaseVersionEqual(*releaseImage.Version, majorMinorVersion)
		if err != nil {
			return nil, err
		}

		if !isMajorMinorEqual {
			continue
		}

		if latestReleaseImages == nil {
			latestReleaseImages = models.ReleaseImages{releaseImage}
		}

		// If it is the same version but different CPU architecture, add the atchitecture to the list.
		// We omit the "-multi" suffix if it exists so multi will be considered the same version.
		if strings.TrimSuffix(*releaseImage.Version, "-multi") == strings.TrimSuffix(*latestReleaseImages[0].Version, "-multi") &&
			!funk.Contains(latestReleaseImages, releaseImage) {
			latestReleaseImages = append(latestReleaseImages, releaseImage)
			continue
		}

		isNewest, err := common.VersionGreaterOrEqual(
			strings.TrimSuffix(*releaseImage.Version, "-multi"),
			strings.TrimSuffix(*latestReleaseImages[0].Version, "-multi"),
		)
		if err != nil {
			return nil, err
		}

		// none-beta > beta, later-beta > beta
		if latestReleaseImages[0].SupportLevel == models.OpenshiftVersionSupportLevelBeta {
			if isNewest || releaseImage.SupportLevel != models.OpenshiftVersionSupportLevelBeta {
				latestReleaseImages = models.ReleaseImages{releaseImage}
			}
		} else { // non-beta-later > non-beta
			if isNewest && releaseImage.SupportLevel != models.OpenshiftVersionSupportLevelBeta {
				latestReleaseImages = models.ReleaseImages{releaseImage}
			}
		}

	}

	if len(latestReleaseImages) == 0 {
		return nil, errors.Errorf("No release image found for version '%s'", majorMinorVersion)
	}

	cpuArchitecturesLiteral := funk.Reduce(latestReleaseImages, func(agg string, releaseImage *models.ReleaseImage) string {
		if agg == "" {
			return *releaseImage.CPUArchitecture
		}
		return fmt.Sprintf("%s, %s", agg, *releaseImage.CPUArchitecture)
	}, "").(string)
	log.Debugf(
		"Found latest release images version '%s' and CPU architectures '%s' for majorMinorVersion '%s'",
		*latestReleaseImages[0].Version,
		cpuArchitecturesLiteral,
		majorMinorVersion,
	)

	return latestReleaseImages, nil
}

func filterReleaseImagesByOnlyLatest(releaseImages models.ReleaseImages, onlyLatest *bool, log logrus.FieldLogger) (models.ReleaseImages, error) {
	if !swag.BoolValue(onlyLatest) {
		return releaseImages, nil
	}

	// Get all relevant major.minor openshift versions
	releaseImagesMajorMinorVersionsSet := map[string]bool{}
	for _, releaseImage := range releaseImages {
		// Typically, releaseImage.OpenshiftVersion follow the major.minor format, though exceptions exist with static release images.
		majorMinorVersion, err := common.GetMajorMinorVersion(*releaseImage.OpenshiftVersion)
		if err != nil {
			return nil, err
		}

		releaseImagesMajorMinorVersionsSet[*majorMinorVersion] = true
	}

	filteredReleaseImages := models.ReleaseImages{}
	for majorMinorVersion := range releaseImagesMajorMinorVersionsSet {
		latestReleaseImagesForMajorMinor, err := getLatestReleaseImagesForMajorMinor(releaseImages, majorMinorVersion, log)
		if err != nil {
			return nil, errors.Wrapf(err, "error occurred while trying to get the latest release images for version: %s", majorMinorVersion)
		}
		filteredReleaseImages = append(filteredReleaseImages, latestReleaseImagesForMajorMinor...)
	}

	return filteredReleaseImages, nil
}

func filterReleaseImagesByVersion(releaseImages models.ReleaseImages, versionPattern *string) models.ReleaseImages {
	if versionPattern == nil {
		return releaseImages
	}

	filterdReleaseImagesInterface := funk.Filter(releaseImages, func(releaseImage *models.ReleaseImage) bool {
		return strings.Contains(*releaseImage.Version, *versionPattern)
	})

	return filterdReleaseImagesInterface.([]*models.ReleaseImage) // models.ReleaseImages does not work for convertion
}

func filterIgnoredVersions(log logrus.FieldLogger, releaseImages models.ReleaseImages, ignoredVersions []string) models.ReleaseImages {
	return funk.Filter(releaseImages, func(releaseImage *models.ReleaseImage) bool {
		version := strings.TrimSuffix(*releaseImage.Version, "-multi")

		majorMinorVersion, err := common.GetMajorMinorVersion(version)
		if err != nil {
			log.WithError(err).Debugf("error occurred while trying to get the major.minor version of '%s', filtering this version out", version)
			return false
		}

		return !funk.Contains(ignoredVersions, version) && !funk.Contains(ignoredVersions, *majorMinorVersion)
	}).([]*models.ReleaseImage)
}

func getReleaseImageSupportLevel(releaseImage *models.ReleaseImage) (*string, error) {
	if releaseImage.SupportLevel != "" {
		return &releaseImage.SupportLevel, nil
	}

	isPreRelease, err := common.IsVersionPreRelease(*releaseImage.Version)
	if err != nil {
		return nil, err
	}

	if *isPreRelease {
		return swag.String(models.ReleaseImageSupportLevelBeta), nil
	}

	return swag.String(models.ReleaseImageSupportLevelProduction), nil
}

func (h *apiHandler) V2ListSupportedOpenshiftVersions(ctx context.Context, params operations.V2ListSupportedOpenshiftVersionsParams) middleware.Responder {
	handler, ok := h.versionsHandler.(*restAPIVersionsHandler)
	// Should not happen
	if !ok {
		return common.GenerateErrorResponder(errors.New("KubeAPI version handler found in restAPI mode"))
	}

	openshiftVersions := models.OpenshiftVersions{}
	hasMultiarchAuthorization := false
	checkedForMultiarchAuthorization := false

	var releaseImages models.ReleaseImages
	err := handler.db.Find(&releaseImages).Error
	if err != nil {
		return common.GenerateErrorResponder(errors.Wrap(err, "error occurred while trying to get release images from DB"))
	}

	releaseImages = filterIgnoredVersions(h.log, releaseImages, handler.ignoredOpenshiftVersions)
	releaseImagesByVersionPattern := filterReleaseImagesByVersion(releaseImages, params.Version)
	releaseImagesByOnlyLatest, err := filterReleaseImagesByOnlyLatest(releaseImages, params.OnlyLatest, h.log)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	// We don't want the order of filtering to have an impact
	releaseImages = funk.Join(releaseImagesByVersionPattern, releaseImagesByOnlyLatest, funk.InnerJoin).([]*models.ReleaseImage)

	for _, releaseImage := range releaseImages {
		// (MGMT-11859) We are filtering out multiarch release images so that they are available
		//              only for customers allowed to use them. This is in order to be able to
		//              expose them in OCP pre-4.13 without making them generally available.
		if len(releaseImage.CPUArchitectures) > 1 {
			if !checkedForMultiarchAuthorization {
				var err error
				hasMultiarchAuthorization, err = h.authzHandler.HasOrgBasedCapability(ctx, ocm.MultiarchCapabilityName)
				if err == nil {
					checkedForMultiarchAuthorization = true
				} else {
					h.log.WithError(err).Errorf("failed to get %s capability", ocm.MultiarchCapabilityName)
					continue
				}
			}
			if !hasMultiarchAuthorization {
				continue
			}
		}

		supportLevel, err := getReleaseImageSupportLevel(releaseImage)
		if err != nil {
			h.log.Debug("error occurred while trying to get the support level of release image version '%s'", *releaseImage.Version)
			continue
		}

		for _, arch := range releaseImage.CPUArchitectures {
			displayName := *releaseImage.Version
			if arch == "" {
				// Empty implies default architecture
				arch = common.DefaultCPUArchitecture
			}

			// In order to mark a specific version and architecture as supported we do not
			// only need to have an available release image, but we need RHCOS image as well.
			if _, err := h.osImages.GetOsImage(displayName, arch); err != nil {
				h.log.Debugf("Marking architecture %s for version %s as not available because no matching OS image found", arch, displayName)
				continue
			}

			// In order to handle multi-arch release images correctly in the UI, we need to tune
			// their DisplayName to contain an appropriate suffix. If the suffix comes from the
			// JSON definition we do nothing, but in case it's missing there (because CVO is not
			// reporting it), we add it ourselves.
			if len(releaseImage.CPUArchitectures) > 1 && !strings.HasSuffix(displayName, "-multi") {
				displayName = displayName + "-multi"
			}

			openshiftVersion, exists := openshiftVersions[displayName]
			if !exists {
				openshiftVersion = models.OpenshiftVersion{
					CPUArchitectures: []string{arch},
					Default:          releaseImage.Default,
					DisplayName:      swag.String(displayName),
					SupportLevel:     supportLevel,
				}
				openshiftVersions[displayName] = openshiftVersion
			} else {
				// For backwards compatibility we handle a scenario when single-arch image exists
				// next to the multi-arch one containing the same architecture. We want to avoid
				// duplicated entry in such a case.
				if !funk.Contains(openshiftVersion.CPUArchitectures, arch) {
					openshiftVersion.CPUArchitectures = append(openshiftVersion.CPUArchitectures, arch)
				}
				openshiftVersion.Default = releaseImage.Default || openshiftVersion.Default
				openshiftVersions[displayName] = openshiftVersion
			}
		}
	}
	return operations.NewV2ListSupportedOpenshiftVersionsOK().WithPayload(openshiftVersions)
}

func (r *apiHandler) V2ListReleaseSources(ctx context.Context, params operations.V2ListReleaseSourcesParams) middleware.Responder {
	return operations.NewV2ListReleaseSourcesOK().WithPayload(r.releaseSources)
}
