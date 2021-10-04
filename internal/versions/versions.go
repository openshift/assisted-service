package versions

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/swag"
	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/versions"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

type MustGatherVersion map[string]string
type MustGatherVersions map[string]MustGatherVersion

type Versions struct {
	SelfVersion     string `envconfig:"SELF_VERSION" default:"quay.io/ocpmetal/assisted-service:latest"`
	AgentDockerImg  string `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/ocpmetal/agent:latest"`
	InstallerImage  string `envconfig:"INSTALLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer:latest"`
	ControllerImage string `envconfig:"CONTROLLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer-controller:latest"`
	ReleaseTag      string `envconfig:"RELEASE_TAG" default:""`
}

//go:generate mockgen -package versions -destination mock_versions.go -self_package github.com/openshift/assisted-service/internal/versions . Handler
type Handler interface {
	restapi.VersionsAPI
	GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error)
	GetReleaseImage(openshiftVersion, cpuArchitecture string) (*models.ReleaseImage, error)
	GetOsImage(openshiftVersion, cpuArchitecture string) (*models.OsImage, error)
	GetLatestOsImage(cpuArchitecture string) (*models.OsImage, error)
	GetCPUArchitectures(openshiftVersion string) ([]string, error)
	GetOpenshiftVersions() []string
	AddReleaseImage(releaseImageUrl, pullSecret string) (*models.ReleaseImage, error)
}

func NewHandler(log logrus.FieldLogger, releaseHandler oc.Release,
	versions Versions, openshiftVersions models.OpenshiftVersions,
	osImages models.OsImages, releaseImages models.ReleaseImages,
	mustGatherVersions MustGatherVersions,
	releaseImageMirror string) (*handler, error) {
	h := &handler{
		versions:           versions,
		openshiftVersions:  openshiftVersions,
		mustGatherVersions: mustGatherVersions,
		osImages:           osImages,
		releaseImages:      releaseImages,
		releaseHandler:     releaseHandler,
		releaseImageMirror: releaseImageMirror,
		log:                log,
	}

	if err := h.validateVersions(); err != nil {
		return nil, err
	}

	return h, nil
}

var _ restapi.VersionsAPI = (*handler)(nil)

type handler struct {
	versions           Versions
	openshiftVersions  models.OpenshiftVersions
	mustGatherVersions MustGatherVersions
	osImages           models.OsImages
	releaseImages      models.ReleaseImages
	releaseHandler     oc.Release
	releaseImageMirror string
	log                logrus.FieldLogger
}

func (h *handler) ListComponentVersions(ctx context.Context, params operations.ListComponentVersionsParams) middleware.Responder {
	return h.V2ListComponentVersions(ctx, operations.V2ListComponentVersionsParams{})
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

func (h *handler) ListSupportedOpenshiftVersions(ctx context.Context, params operations.ListSupportedOpenshiftVersionsParams) middleware.Responder {
	return h.V2ListSupportedOpenshiftVersions(ctx, operations.V2ListSupportedOpenshiftVersionsParams{})
}

func (h *handler) V2ListSupportedOpenshiftVersions(ctx context.Context, params operations.V2ListSupportedOpenshiftVersionsParams) middleware.Responder {
	openshiftVersions := models.OpenshiftVersions{}
	availableOcpVersions := h.GetOpenshiftVersions()
	for _, key := range availableOcpVersions {
		// Fetch available architectures for openshift version
		availableArchitectures, err := h.GetCPUArchitectures(key)
		if err != nil {
			return operations.NewListSupportedOpenshiftVersionsInternalServerError().
				WithPayload(common.GenerateInternalFromError(err))
		}
		// Fetch release image for openshift version (values should be similar across architectures)
		releaseImage, err := h.GetReleaseImage(key, common.DefaultCPUArchitecture)
		if err != nil {
			return operations.NewListSupportedOpenshiftVersionsInternalServerError().
				WithPayload(common.GenerateInternalFromError(err))
		}

		openshiftVersion := models.OpenshiftVersion{
			CPUArchitectures: availableArchitectures,
			Default:          releaseImage.Default,
			DisplayName:      *releaseImage.Version,
			SupportLevel:     h.getSupportLevel(*releaseImage.Version),
		}
		openshiftVersions[key] = openshiftVersion
	}

	return operations.NewV2ListSupportedOpenshiftVersionsOK().WithPayload(openshiftVersions)
}

func (h *handler) GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error) {
	versionKey, err := h.getKey(openshiftVersion)
	if err != nil {
		return nil, err
	}
	if !funk.Contains(h.GetOpenshiftVersions(), versionKey) {
		return nil, errors.Errorf("No operators must-gather version for unsupported openshift version %s", versionKey)
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

// Returns the latest OSImage entity for a specified CPU architecture
func (h *handler) GetLatestOsImage(cpuArchitecture string) (*models.OsImage, error) {
	var latest *models.OsImage
	openshiftVersions := h.GetOpenshiftVersions()
	for _, k := range openshiftVersions {
		osImage, err := h.GetOsImage(k, cpuArchitecture)
		if err != nil {
			continue
		}
		if latest == nil || *osImage.OpenshiftVersion > *latest.OpenshiftVersion {
			latest = osImage
		}
	}
	if latest == nil {
		return nil, errors.Errorf("No OS images are available")
	}
	return latest, nil
}

// Returns the OsImage entity
func (h *handler) GetOsImage(openshiftVersion, cpuArchitecture string) (*models.OsImage, error) {
	versionKey, err := h.getKey(openshiftVersion)
	if err != nil {
		return nil, err
	}

	for _, osImage := range h.osImages {
		if osImage.OpenshiftVersion == nil {
			return nil, errors.Errorf("Missing openshift_version in OsImage")
		}
		if *osImage.OpenshiftVersion == versionKey && *osImage.CPUArchitecture == cpuArchitecture {
			return osImage, nil
		}
	}

	if cpuArchitecture != "" && cpuArchitecture != common.DefaultCPUArchitecture {
		// An empty cpuArchitecture implies the default CPU architecture.
		// TODO: remove this check once release images list is exclusively used.
		return nil, errors.Errorf("The requested CPU architecture (%s) isn't specified in OS images list", cpuArchitecture)
	}

	// Try fetching from OpenshiftVersion struct for backwards compatibility
	if !h.isOpenshiftVersionSupported(versionKey) {
		return nil, errors.Errorf("The requested openshift version (%s) isn't specified in versions list", versionKey)
	}
	ocpVersion := h.openshiftVersions[versionKey]
	osImage := models.OsImage{
		OpenshiftVersion: &versionKey,
		URL:              &ocpVersion.RhcosImage,
		RootfsURL:        &ocpVersion.RhcosRootfs,
		Version:          &ocpVersion.RhcosVersion,
	}

	return &osImage, nil
}

// Returns the ReleaseImage entity
func (h *handler) GetReleaseImage(openshiftVersion, cpuArchitecture string) (*models.ReleaseImage, error) {
	versionKey, err := h.getKey(openshiftVersion)
	if err != nil {
		return nil, err
	}

	for _, releaseImage := range h.releaseImages {
		if *releaseImage.OpenshiftVersion == versionKey && *releaseImage.CPUArchitecture == cpuArchitecture {
			return releaseImage, nil
		}
	}

	if cpuArchitecture != "" && cpuArchitecture != common.DefaultCPUArchitecture {
		// An empty cpuArchitecture implies the default CPU architecture.
		return nil, errors.Errorf("The requested CPU architecture (%s) isn't specified in Release images list", cpuArchitecture)
	}

	// Try fetching from OpenshiftVersion struct for backwards compatibility
	if !h.isOpenshiftVersionSupported(versionKey) {
		return nil, errors.Errorf("The requested openshift version (%s) isn't specified in versions list", versionKey)
	}
	ocpVersion := h.openshiftVersions[versionKey]
	releaseImage := models.ReleaseImage{
		CPUArchitecture:  &cpuArchitecture,
		OpenshiftVersion: &versionKey,
		URL:              &ocpVersion.ReleaseImage,
		Version:          &ocpVersion.ReleaseVersion,
	}

	return &releaseImage, nil
}

func (h *handler) AddReleaseImage(releaseImageUrl, pullSecret string) (*models.ReleaseImage, error) {
	// Get openshift version from release image metadata (oc adm release info)
	ocpReleaseVersion, err := h.releaseHandler.GetOpenshiftVersion(h.log, releaseImageUrl, "", pullSecret)
	if err != nil {
		return nil, err
	}

	// Get CPU architecture from release image
	cpuArchitecture, err := h.releaseHandler.GetReleaseArchitecture(h.log, releaseImageUrl, pullSecret)
	if err != nil {
		return nil, err
	}

	// Get minor version
	ocpVersionKey, err := h.getKey(ocpReleaseVersion)
	if err != nil {
		return nil, err
	}

	// Ensure a relevant OsImage exists
	osImage, err := h.GetOsImage(ocpVersionKey, cpuArchitecture)
	if err != nil || osImage.URL == nil {
		return nil, errors.Errorf("No OS images are available for version: %s", ocpVersionKey)
	}

	// Fetch ReleaseImage if exists
	releaseImage, err := h.GetReleaseImage(ocpVersionKey, cpuArchitecture)
	if err != nil {
		// Create a new ReleaseImage
		releaseImage = &models.ReleaseImage{
			OpenshiftVersion: &ocpVersionKey,
			CPUArchitecture:  &cpuArchitecture,
		}
	} else {
		// Remove original ReleaseImage from array
		h.releaseImages = funk.Subtract(h.releaseImages, models.ReleaseImages{releaseImage}).([]*models.ReleaseImage)
	}

	// Update ReleaseImage
	releaseImage.URL = &releaseImageUrl
	releaseImage.Version = &ocpReleaseVersion

	// Store in releaseImages array
	h.releaseImages = append(h.releaseImages, releaseImage)
	h.log.Infof("Stored release version: %s", ocpReleaseVersion)

	return releaseImage, nil
}

// Get CPU architectures available for the specified openshift version
// according to the OS images list.
func (h *handler) GetCPUArchitectures(openshiftVersion string) ([]string, error) {
	versionKey, err := h.getKey(openshiftVersion)
	if err != nil {
		return nil, err
	}
	cpuArchitectures := []string{}
	for _, release := range h.osImages {
		if *release.OpenshiftVersion == versionKey {
			if release.OpenshiftVersion == nil {
				return nil, errors.Errorf("Missing openshift_version in OsImage")
			}
			if release.CPUArchitecture == nil {
				return nil, errors.Errorf("Missing cpu_architecture in OsImage")
			}
			cpuArchitectures = append(cpuArchitectures, *release.CPUArchitecture)
		}
	}

	// TODO: remove once osImages list is exclusively used
	if len(cpuArchitectures) == 0 {
		if !h.isOpenshiftVersionSupported(openshiftVersion) {
			return nil, errors.Errorf("No OS images for openshift version %s", openshiftVersion)
		}
		cpuArchitectures = append(cpuArchitectures, common.DefaultCPUArchitecture)
	}

	return cpuArchitectures, nil
}

// Get available openshift versions according to OS images list.
// Or, keys of openshiftVersions map as a fallback.
func (h *handler) GetOpenshiftVersions() []string {
	versions := []string{}
	for _, image := range h.osImages {
		if !funk.Contains(versions, *image.OpenshiftVersion) {
			versions = append(versions, *image.OpenshiftVersion)
		}
	}
	if len(versions) == 0 {
		for k := range h.openshiftVersions {
			versions = append(versions, k)
		}
	}
	return versions
}

// Returns version in major.minor format
func (h *handler) getKey(openshiftVersion string) (string, error) {
	v, err := version.NewVersion(openshiftVersion)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d", v.Segments()[0], v.Segments()[1]), nil
}

// Checks whether a specified version is in the openshiftVersions map
func (h *handler) isOpenshiftVersionSupported(openshiftVersion string) bool {
	versionKey, err := h.getKey(openshiftVersion)
	if err != nil {
		return false
	}
	if _, ok := h.openshiftVersions[versionKey]; !ok {
		return false
	}

	return true
}

func (h *handler) getSupportLevel(openshiftVersion string) string {
	preReleases := []string{"-fc", "-rc", "nightly"}
	for _, preRelease := range preReleases {
		if strings.Contains(openshiftVersion, preRelease) {
			return models.OpenshiftVersionSupportLevelBeta
		}
	}
	return models.OpenshiftVersionSupportLevelProduction
}

// Ensure no missing values in OS images and Release images.
func (h *handler) validateVersions() error {
	missingValueTemplate := "Missing value in OSImage for '%s' field (openshift_version: %s)"
	for _, key := range h.GetOpenshiftVersions() {
		architectures, err := h.GetCPUArchitectures(key)
		if err != nil {
			return err
		}

		for _, architecture := range architectures {
			osImage, err := h.GetOsImage(key, architecture)
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("Failed to get OSImage for openshift version: %s", key))
			}
			if swag.StringValue(osImage.URL) == "" {
				return errors.Errorf(fmt.Sprintf(missingValueTemplate, "url", key))
			}
			if swag.StringValue(osImage.RootfsURL) == "" {
				return errors.Errorf(fmt.Sprintf(missingValueTemplate, "rootfs_url", key))
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
