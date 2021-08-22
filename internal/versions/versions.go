package versions

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-openapi/runtime/middleware"
	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/versions"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
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
	GetOpenshiftVersion(openshiftVersion, cpuArchitecture string) (*models.OpenshiftVersion, error)
	GetOsImage(openshiftVersion string) (*models.OsImage, error)
	GetKey(openshiftVersion string) (string, error)
	IsOpenshiftVersionSupported(versionKey string) bool
	AddOpenshiftVersion(ocpReleaseImage, pullSecret string) (*models.OpenshiftVersion, error)
	GetCPUArchitectures(openshiftVersion string) ([]string, error)
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
	return operations.NewListComponentVersionsOK().WithPayload(
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
	return operations.NewListSupportedOpenshiftVersionsOK().WithPayload(h.openshiftVersions)
}

func (h *handler) GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return nil, err
	}
	if !h.IsOpenshiftVersionSupported(versionKey) {
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
	ocpVersion, err := h.GetOpenshiftVersion(openshiftVersion, cpuArchitecture)
	if err != nil {
		return nil, err
	}
	ocpMustGatherImage, err := h.releaseHandler.GetMustGatherImage(h.log, *ocpVersion.ReleaseImage, h.releaseImageMirror, pullSecret)
	if err != nil {
		return nil, err
	}
	h.mustGatherVersions[versionKey]["ocp"] = ocpMustGatherImage

	versions := h.mustGatherVersions[versionKey]
	return versions, nil
}

func (h *handler) IsOpenshiftVersionSupported(versionKey string) bool {
	if _, ok := h.openshiftVersions[versionKey]; !ok {
		return false
	}

	return true
}

// Returns the OpenshiftVersion entity
func (h *handler) GetOpenshiftVersion(openshiftVersion, cpuArchitecture string) (*models.OpenshiftVersion, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return nil, err
	}
	if !h.IsOpenshiftVersionSupported(versionKey) {
		return nil, errors.Errorf("The requested openshift version (%s) isn't specified in versions list", versionKey)
	}

	missingValueTemplate := "Missing value in OpenshiftVersion for '%s' field"
	ocpVersion := h.openshiftVersions[versionKey]
	if ocpVersion.DisplayName == nil {
		return nil, errors.Errorf(fmt.Sprintf(missingValueTemplate, "display_name"))
	}
	if ocpVersion.SupportLevel == nil {
		return nil, errors.Errorf(fmt.Sprintf(missingValueTemplate, "support_level"))
	}

	// Get release image URL and version from releaseImages if available
	for _, release := range h.releaseImages {
		if *release.OpenshiftVersion == versionKey && *release.CPUArchitecture == cpuArchitecture {
			ocpVersion.ReleaseImage = release.URL
			ocpVersion.ReleaseVersion = release.Version
			return &ocpVersion, nil
		}
	}

	if cpuArchitecture != "" && cpuArchitecture != common.DefaultCPUArchitecture {
		// An empty cpuArchitecture implies the default CPU architecture.
		// TODO: remove this check once release images list is exclusively used.
		return nil, errors.Errorf("The requested CPU architecture (%s) isn't specified in release images list", cpuArchitecture)
	}

	if ocpVersion.ReleaseImage == nil {
		return nil, errors.Errorf(fmt.Sprintf(missingValueTemplate, "release_image"))
	}
	if ocpVersion.ReleaseVersion == nil {
		return nil, errors.Errorf(fmt.Sprintf(missingValueTemplate, "release_version"))
	}

	return &ocpVersion, nil
}

// Returns the OsImage entity
func (h *handler) GetOsImage(openshiftVersion string) (*models.OsImage, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return nil, err
	}
	if !h.IsOpenshiftVersionSupported(versionKey) {
		return nil, errors.Errorf("The requested openshift version (%s) isn't specified in versions list", versionKey)
	}

	for _, osImage := range h.osImages {
		if *osImage.OpenshiftVersion == versionKey {
			return osImage, nil
		}
	}

	// Try fetching from OpenshiftVersion struct for backwards compatibility
	ocpVersion := h.openshiftVersions[versionKey]
	osImage := models.OsImage{
		OpenshiftVersion: &versionKey,
		URL:              ocpVersion.RhcosImage,
		RootfsURL:        ocpVersion.RhcosRootfs,
		Version:          ocpVersion.RhcosVersion,
	}

	return &osImage, nil
}

// Returns version in major.minor format
func (h *handler) GetKey(openshiftVersion string) (string, error) {
	v, err := version.NewVersion(openshiftVersion)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d", v.Segments()[0], v.Segments()[1]), nil
}

func (h *handler) AddOpenshiftVersion(ocpReleaseImage, pullSecret string) (*models.OpenshiftVersion, error) {
	// Check whether ocpReleaseImage already exists in cache
	for _, v := range h.openshiftVersions {
		if v.ReleaseImage != nil && *v.ReleaseImage == ocpReleaseImage {
			// Return existing version
			version := v
			return &version, nil
		}
	}

	// Get openshift version from release image metadata (oc adm release info)
	ocpReleaseVersion, err := h.releaseHandler.GetOpenshiftVersion(h.log, ocpReleaseImage, h.releaseImageMirror, pullSecret)
	if err != nil {
		return nil, err
	}

	// Return if version is not specified in OPENSHIFT_VERSIONS
	ocpVersionKey, err := h.GetKey(ocpReleaseVersion)
	if err != nil {
		return nil, err
	}
	versionFromCache, ok := h.openshiftVersions[ocpVersionKey]
	if !ok {
		return nil, errors.Errorf("RHCOS image is not configured for version: %s, supported versions: %s",
			ocpVersionKey, reflect.ValueOf(h.openshiftVersions).MapKeys())
	}

	// Get SupportLevel or default to 'custom'
	var supportLevel string
	if versionFromCache.SupportLevel != nil {
		supportLevel = *versionFromCache.SupportLevel
	}
	supportLevel = models.OpenshiftVersionSupportLevelCustom

	// Create OpenshiftVersion according to fetched data
	openshiftVersion := &models.OpenshiftVersion{
		DisplayName:    &ocpReleaseVersion,
		ReleaseImage:   &ocpReleaseImage,
		ReleaseVersion: &ocpReleaseVersion,
		RhcosImage:     versionFromCache.RhcosImage,
		RhcosRootfs:    versionFromCache.RhcosRootfs,
		RhcosVersion:   versionFromCache.RhcosVersion,
		SupportLevel:   &supportLevel,
	}

	// Store in map
	h.openshiftVersions[ocpVersionKey] = *openshiftVersion
	h.log.Infof("Stored OCP version: %s", ocpReleaseVersion)

	return openshiftVersion, nil
}

// Get CPU architecture available by the specified openshift version
func (h *handler) GetCPUArchitectures(openshiftVersion string) ([]string, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return nil, err
	}
	if !h.IsOpenshiftVersionSupported(versionKey) {
		return nil, errors.Errorf("No available CPU architectures for openshift version %s", versionKey)
	}

	cpuArchitectures := []string{}
	for _, release := range h.releaseImages {
		if *release.OpenshiftVersion == openshiftVersion {
			cpuArchitectures = append(cpuArchitectures, *release.CPUArchitecture)
		}
	}

	// TODO: remove once releaseImages list is exclusively used
	if len(cpuArchitectures) == 0 {
		cpuArchitectures = append(cpuArchitectures, common.DefaultCPUArchitecture)
	}

	return cpuArchitectures, nil
}

// Ensures no missing values in OS images.
// No need to validate OpenshiftVersion fields here,
// e.g. since release is not available in AddOpenshiftVersion flow.
func (h *handler) validateVersions() error {
	missingValueTemplate := "Missing value in OsImage for '%s' field"
	for key := range h.openshiftVersions {
		osImage, err := h.GetOsImage(key)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("Failed to get OSImage for openshift version: %s", key))
		}

		if osImage.URL == nil {
			return errors.Errorf(fmt.Sprintf(missingValueTemplate, "url"))
		}
		if osImage.RootfsURL == nil {
			return errors.Errorf(fmt.Sprintf(missingValueTemplate, "rootfs_url"))
		}
		if osImage.Version == nil {
			return errors.Errorf(fmt.Sprintf(missingValueTemplate, "version"))
		}
	}
	missingValueTemplate = "Missing value in ReleaseImage for '%s' field"
	for _, release := range h.releaseImages {
		if release.URL == nil {
			return errors.Errorf(fmt.Sprintf(missingValueTemplate, "url"))
		}
		if release.Version == nil {
			return errors.Errorf(fmt.Sprintf(missingValueTemplate, "version"))
		}
		if release.CPUArchitecture == nil {
			return errors.Errorf(fmt.Sprintf(missingValueTemplate, "cpu_architecture"))
		}
	}

	return nil
}
