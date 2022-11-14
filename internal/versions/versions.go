package versions

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/swag"
	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/versions"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

type MustGatherVersion map[string]string
type MustGatherVersions map[string]MustGatherVersion

type Versions struct {
	SelfVersion     string `envconfig:"SELF_VERSION" default:"Unknown"`
	AgentDockerImg  string `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer-agent:latest"`
	InstallerImage  string `envconfig:"INSTALLER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer:latest"`
	ControllerImage string `envconfig:"CONTROLLER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer-controller:latest"`
	ReleaseTag      string `envconfig:"RELEASE_TAG" default:""`
}

//go:generate mockgen --build_flags=--mod=mod -package versions -destination mock_versions.go -self_package github.com/openshift/assisted-service/internal/versions . Handler
type Handler interface {
	restapi.VersionsAPI
	GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error)
	GetReleaseImage(openshiftVersion, cpuArchitecture string) (*models.ReleaseImage, error)
	GetDefaultReleaseImage(cpuArchitecture string) (*models.ReleaseImage, error)
	GetOsImage(openshiftVersion, cpuArchitecture string) (*models.OsImage, error)
	GetLatestOsImage(cpuArchitecture string) (*models.OsImage, error)
	GetOsImageOrLatest(version string, cpuArch string) (*models.OsImage, error)
	GetCPUArchitectures(openshiftVersion string) []string
	GetOpenshiftVersions() []string
	AddReleaseImage(releaseImageUrl, pullSecret, ocpReleaseVersion string, cpuArchitectures []string) (*models.ReleaseImage, error)
	ValidateAccessToMultiarch(ctx context.Context, authzHandler auth.Authorizer) error
	ValidateReleaseImageForRHCOS(rhcosVersion, cpuArch string) error
}

func NewHandler(log logrus.FieldLogger, releaseHandler oc.Release,
	versions Versions, osImages models.OsImages, releaseImages models.ReleaseImages,
	mustGatherVersions MustGatherVersions,
	releaseImageMirror string, authzHandler auth.Authorizer) (*handler, error) {
	h := &handler{
		versions:           versions,
		mustGatherVersions: mustGatherVersions,
		osImages:           osImages,
		releaseImages:      releaseImages,
		releaseHandler:     releaseHandler,
		releaseImageMirror: releaseImageMirror,
		log:                log,
		authzHandler:       authzHandler,
	}

	if err := h.validateVersions(); err != nil {
		return nil, err
	}

	return h, nil
}

var _ restapi.VersionsAPI = (*handler)(nil)

type handler struct {
	versions           Versions
	mustGatherVersions MustGatherVersions
	osImages           models.OsImages
	releaseImages      models.ReleaseImages
	releaseHandler     oc.Release
	releaseImageMirror string
	log                logrus.FieldLogger
	authzHandler       auth.Authorizer
}

func (h *handler) V2ListComponentVersions(ctx context.Context, params operations.V2ListComponentVersionsParams) middleware.Responder {
	return operations.NewV2ListComponentVersionsOK().WithPayload(
		&models.ListVersions{
			Versions: models.Versions{
				"assisted-installer-service":    h.versions.SelfVersion,
				"discovery-agent":               h.versions.AgentDockerImg,
				"assisted-installer":            h.versions.InstallerImage,
				"assisted-installer-controller": h.versions.ControllerImage,
			},
			ReleaseTag: h.versions.ReleaseTag,
		})
}

func (h *handler) V2ListSupportedOpenshiftVersions(ctx context.Context, params operations.V2ListSupportedOpenshiftVersionsParams) middleware.Responder {
	openshiftVersions := models.OpenshiftVersions{}
	hasMultiarchAuthorization := false
	checkedForMultiarchAuthorization := false

	for _, releaseImage := range h.releaseImages {
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
			if !checkedForMultiarchAuthorization {
				checkedForMultiarchAuthorization = true
				if err := h.ValidateAccessToMultiarch(ctx, h.authzHandler); err != nil {
					if strings.Contains(err.Error(), "multiarch clusters are not available") {
						continue
					} else {
						return common.GenerateErrorResponder(err)
					}
				}
				hasMultiarchAuthorization = true
			}
			if !hasMultiarchAuthorization {
				continue
			}
		}

		for _, arch := range supportedArchs {
			key := *releaseImage.OpenshiftVersion
			if arch == "" {
				// Empty implies default architecture
				arch = common.DefaultCPUArchitecture
			}

			// In order to mark a specific version and architecture as supported we do not
			// only need to have an available release image, but we need RHCOS image as well.
			if _, err := h.GetOsImage(key, arch); err != nil {
				h.log.Debugf("Marking architecture %s for version %s as not available because no matching OS image found", arch, key)
				continue
			}

			openshiftVersion, exists := openshiftVersions[key]
			if !exists {
				openshiftVersion = models.OpenshiftVersion{
					CPUArchitectures: []string{arch},
					Default:          releaseImage.Default,
					DisplayName:      releaseImage.Version,
					SupportLevel:     getSupportLevel(*releaseImage),
				}
				openshiftVersions[key] = openshiftVersion
			} else {
				// For backwards compatibility we handle a scenario when single-arch image exists
				// next to the multi-arch one containing the same architecture. We want to avoid
				// duplicated entry in such a case.
				exists := func(slice []string, x string) bool {
					for _, elem := range slice {
						if x == elem {
							return true
						}
					}
					return false
				}
				if !exists(openshiftVersion.CPUArchitectures, arch) {
					openshiftVersion.CPUArchitectures = append(openshiftVersion.CPUArchitectures, arch)
				}
				openshiftVersion.Default = releaseImage.Default || openshiftVersion.Default
				openshiftVersions[key] = openshiftVersion
			}
		}
	}
	return operations.NewV2ListSupportedOpenshiftVersionsOK().WithPayload(openshiftVersions)
}

func (h *handler) GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error) {
	versionKey, err := getKey(openshiftVersion)
	if err != nil {
		return nil, err
	}
	if h.mustGatherVersions == nil {
		h.mustGatherVersions = make(MustGatherVersions)
	}
	if h.mustGatherVersions[versionKey] == nil {
		h.mustGatherVersions[versionKey] = make(MustGatherVersion)
	}

	//check if ocp must-gather image is already in the cache
	if h.mustGatherVersions[versionKey]["ocp"] != "" {
		versions := h.mustGatherVersions[versionKey]
		return versions, nil
	}
	//if not, fetch it from the release image and add it to the cache
	releaseImage, err := h.GetReleaseImage(openshiftVersion, cpuArchitecture)
	if err != nil {
		return nil, err
	}
	ocpMustGatherImage, err := h.releaseHandler.GetMustGatherImage(h.log, *releaseImage.URL, h.releaseImageMirror, pullSecret)
	if err != nil {
		return nil, err
	}
	h.mustGatherVersions[versionKey]["ocp"] = ocpMustGatherImage

	versions := h.mustGatherVersions[versionKey]
	return versions, nil
}

// Returns the default ReleaseImage entity for a specified CPU architecture
func (h *handler) GetDefaultReleaseImage(cpuArchitecture string) (*models.ReleaseImage, error) {
	defaultReleaseImage := funk.Find(h.releaseImages, func(releaseImage *models.ReleaseImage) bool {
		return releaseImage.Default && *releaseImage.CPUArchitecture == cpuArchitecture
	})

	if defaultReleaseImage == nil {
		return nil, errors.Errorf("Default release image is not available")
	}

	return defaultReleaseImage.(*models.ReleaseImage), nil
}

// Returns the OsImage entity
func (h *handler) GetOsImage(openshiftVersion, cpuArchitecture string) (*models.OsImage, error) {
	if cpuArchitecture == "" {
		// Empty implies default CPU architecture
		cpuArchitecture = common.DefaultCPUArchitecture
	}
	// Filter OS images by specified CPU architecture
	osImages := funk.Filter(h.osImages, func(osImage *models.OsImage) bool {
		if swag.StringValue(osImage.CPUArchitecture) == "" {
			return cpuArchitecture == common.DefaultCPUArchitecture
		}
		return swag.StringValue(osImage.CPUArchitecture) == cpuArchitecture
	})
	if funk.IsEmpty(osImages) {
		return nil, errors.Errorf("The requested CPU architecture (%s) isn't specified in OS images list", cpuArchitecture)
	}

	// Search for specified x.y.z openshift version
	osImage := funk.Find(osImages, func(osImage *models.OsImage) bool {
		return swag.StringValue(osImage.OpenshiftVersion) == openshiftVersion
	})

	versionKey, err := getKey(openshiftVersion)
	if err != nil {
		return nil, err
	}

	if osImage == nil {
		// Fallback to x.y version
		osImage = funk.Find(osImages, func(osImage *models.OsImage) bool {
			return *osImage.OpenshiftVersion == versionKey
		})
	}

	if osImage == nil {
		// Find latest available patch version by x.y version
		osImages := funk.Filter(osImages, func(osImage *models.OsImage) bool {
			imageVersionKey, err := getKey(*osImage.OpenshiftVersion)
			if err != nil {
				return false
			}
			return imageVersionKey == versionKey
		}).([]*models.OsImage)
		sort.Slice(osImages, func(i, j int) bool {
			v1, _ := version.NewVersion(*osImages[i].OpenshiftVersion)
			v2, _ := version.NewVersion(*osImages[j].OpenshiftVersion)
			return v1.GreaterThan(v2)
		})
		if !funk.IsEmpty(osImages) {
			osImage = osImages[0]
		}
	}

	if osImage != nil {
		return osImage.(*models.OsImage), nil
	}

	return nil, errors.Errorf(
		"The requested OS image for version (%s) and CPU architecture (%s) isn't specified in OS images list",
		openshiftVersion, cpuArchitecture)
}

// Returns the ReleaseImage entity
func (h *handler) GetReleaseImage(openshiftVersion, cpuArchitecture string) (*models.ReleaseImage, error) {
	if cpuArchitecture == "" {
		// Empty implies default CPU architecture
		cpuArchitecture = common.DefaultCPUArchitecture
	}
	// Filter Release images by specified CPU architecture.
	releaseImages := funk.Filter(h.releaseImages, func(releaseImage *models.ReleaseImage) bool {
		for _, arch := range releaseImage.CPUArchitectures {
			if arch == cpuArchitecture {
				return true
			}
		}
		return swag.StringValue(releaseImage.CPUArchitecture) == cpuArchitecture
	})
	if funk.IsEmpty(releaseImages) {
		return nil, errors.Errorf("The requested CPU architecture (%s) isn't specified in release images list", cpuArchitecture)
	}
	// Search for specified x.y.z openshift version
	releaseImage := funk.Find(releaseImages, func(releaseImage *models.ReleaseImage) bool {
		return *releaseImage.OpenshiftVersion == openshiftVersion
	})

	if releaseImage == nil {
		// Fallback to x.y version
		versionKey, err := getKey(openshiftVersion)
		if err != nil {
			return nil, err
		}
		releaseImage = funk.Find(releaseImages, func(releaseImage *models.ReleaseImage) bool {
			return *releaseImage.OpenshiftVersion == versionKey
		})
	}

	if releaseImage != nil {
		return releaseImage.(*models.ReleaseImage), nil
	}

	return nil, errors.Errorf(
		"The requested release image for version (%s) and CPU architecture (%s) isn't specified in release images list",
		openshiftVersion, cpuArchitecture)
}

// ValidateReleaseImageForRHCOS validates whether for a specified RHCOS version we have an OCP
// version that can be used. This functions performs a very weak matching because RHCOS versions
// are very loosely coupled with the OpenShift versions what allows for a variety of mix&match.
func (h *handler) ValidateReleaseImageForRHCOS(rhcosVersion, cpuArchitecture string) error {
	rhcosVersion, err := getKey(rhcosVersion)
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
				minorVersion, err := getKey(*releaseImage.OpenshiftVersion)
				if err != nil {
					return err
				}
				if minorVersion == rhcosVersion {
					h.log.Debugf("Validator for the architecture %s found the following OCP version: %s", cpuArchitecture, *releaseImage.Version)
					return nil
				}
			}
		}
	}

	return errors.Errorf("The requested RHCOS version (%s, arch: %s) does not have a matching OpenShift release image", rhcosVersion, cpuArchitecture)
}

// Returns the latest OSImage entity for a specified CPU architecture
func (h *handler) GetLatestOsImage(cpuArchitecture string) (*models.OsImage, error) {
	var latest *models.OsImage
	openshiftVersions := h.GetOpenshiftVersions()
	for _, k := range openshiftVersions {
		osImage, err := h.GetOsImage(k, cpuArchitecture)
		if err != nil {
			continue
		}
		if latest == nil {
			latest = osImage
		} else {
			imageVer, _ := version.NewVersion(*osImage.OpenshiftVersion)
			latestVer, _ := version.NewVersion(*latest.OpenshiftVersion)
			if imageVer.GreaterThan(latestVer) {
				latest = osImage
			}
		}
	}
	if latest == nil {
		return nil, errors.Errorf("No OS images are available")
	}
	return latest, nil
}

func (h *handler) GetOsImageOrLatest(version string, cpuArch string) (*models.OsImage, error) {
	var osImage *models.OsImage
	var err error
	if version != "" {
		osImage, err = h.GetOsImage(version, cpuArch)
		if err != nil {
			return nil, errors.Wrapf(err, "No OS image for Openshift version %s and architecture %s", version, cpuArch)
		}
	} else {
		osImage, err = h.GetLatestOsImage(cpuArch)
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to get latest OS image for architecture %s", cpuArch)
		}
	}
	return osImage, nil
}

func (h *handler) AddReleaseImage(releaseImageUrl, pullSecret, ocpReleaseVersion string, cpuArchitectures []string) (*models.ReleaseImage, error) {
	var err error
	var cpuArchitecture string
	var osImage *models.OsImage

	// If release version or cpu architectures are not specified, use oc to fetch values.
	// If cpu architecture is passed as "multiarch" instead of unwrapped architectures, recalculate it.
	if ocpReleaseVersion == "" || len(cpuArchitectures) == 0 || cpuArchitectures[0] == common.MultiCPUArchitecture {
		// Get openshift version from release image metadata (oc adm release info)
		ocpReleaseVersion, err = h.releaseHandler.GetOpenshiftVersion(h.log, releaseImageUrl, "", pullSecret)
		if err != nil {
			return nil, err
		}
		h.log.Debugf("For release image %s detected version: %s", releaseImageUrl, ocpReleaseVersion)

		// Get CPU architecture from release image. For single-arch image the returned list will contain
		// as single entry with the architecture. For multi-arch image the list will contain all the architectures
		// that the image references to.
		cpuArchitectures, err = h.releaseHandler.GetReleaseArchitecture(h.log, releaseImageUrl, "", pullSecret)
		if err != nil {
			return nil, err
		}
		h.log.Debugf("For release image %s detected architecture: %s", releaseImageUrl, cpuArchitectures)
	}

	if len(cpuArchitectures) == 1 {
		cpuArchitecture = cpuArchitectures[0]

		// Ensure a relevant OsImage exists. For multiarch we disabling the code below because we don't know yet
		// what is going to be the architecture of InfraEnv and Agent.
		osImage, err = h.GetOsImage(ocpReleaseVersion, cpuArchitecture)
		if err != nil || osImage.URL == nil {
			return nil, errors.Errorf("No OS images are available for version %s and architecture %s", ocpReleaseVersion, cpuArchitecture)
		}
	} else {
		cpuArchitecture = common.MultiCPUArchitecture
	}

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

// Get CPU architectures available for the specified openshift version
// according to the OS images list.
func (h *handler) GetCPUArchitectures(openshiftVersion string) []string {
	cpuArchitectures := []string{}
	versionKey, err := getKey(openshiftVersion)
	if err != nil {
		return cpuArchitectures
	}
	for _, osImage := range h.osImages {
		if *osImage.OpenshiftVersion == openshiftVersion || *osImage.OpenshiftVersion == versionKey {
			if swag.StringValue(osImage.CPUArchitecture) == "" {
				// Empty or missing property implies default CPU architecture
				defaultArch := common.DefaultCPUArchitecture
				osImage.CPUArchitecture = &defaultArch
			}
			if !funk.Contains(cpuArchitectures, *osImage.CPUArchitecture) {
				cpuArchitectures = append(cpuArchitectures, *osImage.CPUArchitecture)
			}
		}
	}
	return cpuArchitectures
}

// Get available openshift versions according to OS images list.
func (h *handler) GetOpenshiftVersions() []string {
	versions := []string{}
	for _, image := range h.osImages {
		if !funk.Contains(versions, *image.OpenshiftVersion) {
			versions = append(versions, *image.OpenshiftVersion)
		}
	}
	return versions
}

func (h *handler) ValidateAccessToMultiarch(ctx context.Context, authzHandler auth.Authorizer) error {
	var err error
	var multiarchAllowed bool

	multiarchAllowed, err = authzHandler.HasOrgBasedCapability(ctx, ocm.MultiarchCapabilityName)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("error getting user %s capability, error: %w", ocm.MultiarchCapabilityName, err))
	}
	if !multiarchAllowed {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("%s", "multiarch clusters are not available"))
	}
	return nil
}

// Returns version in major.minor format
func getKey(openshiftVersion string) (string, error) {
	v, err := version.NewVersion(openshiftVersion)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d", v.Segments()[0], v.Segments()[1]), nil
}

func getSupportLevel(releaseImage models.ReleaseImage) *string {
	if releaseImage.SupportLevel != "" {
		return &releaseImage.SupportLevel
	}

	preReleases := []string{"-fc", "-rc", "nightly"}
	for _, preRelease := range preReleases {
		if strings.Contains(*releaseImage.Version, preRelease) {
			return swag.String(models.OpenshiftVersionSupportLevelBeta)
		}
	}
	return swag.String(models.OpenshiftVersionSupportLevelProduction)
}

// Ensure no missing values in OS images and Release images.
func (h *handler) validateVersions() error {
	for _, osImage := range h.osImages {
		if swag.StringValue(osImage.OpenshiftVersion) == "" {
			return errors.Errorf("Missing openshift_version in OsImage: %v", osImage)
		}
	}

	openshiftVersions := h.GetOpenshiftVersions()
	if len(openshiftVersions) == 0 {
		return errors.Errorf("No OS images are available")
	}

	missingValueTemplate := "Missing value in OSImage for '%s' field (openshift_version: %s)"
	for _, key := range openshiftVersions {
		architectures := h.GetCPUArchitectures(key)
		for _, architecture := range architectures {
			osImage, err := h.GetOsImage(key, architecture)
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("Failed to get OSImage for openshift version: %s", key))
			}
			if swag.StringValue(osImage.URL) == "" {
				return errors.Errorf(fmt.Sprintf(missingValueTemplate, "url", key))
			}
			if swag.StringValue(osImage.Version) == "" {
				return errors.Errorf(fmt.Sprintf(missingValueTemplate, "version", key))
			}
		}
	}

	// Release images are not mandatory (dynamically added in kube-api flow),
	// validating fields for those specified in list.
	missingValueTemplate = "Missing value in ReleaseImage for '%s' field"
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
	}

	return nil
}
