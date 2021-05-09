package versions

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-openapi/runtime/middleware"
	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/versions"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

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
	GetReleaseImage(openshiftVersion string) (string, error)
	GetRHCOSImage(openshiftVersion string) (string, error)
	GetRHCOSVersion(openshiftVersion string) (string, error)
	GetReleaseVersion(openshiftVersion string) (string, error)
	GetKey(openshiftVersion string) (string, error)
	GetVersion(openshiftVersion string) (*models.OpenshiftVersion, error)
	IsOpenshiftVersionSupported(versionKey string) bool
	AddOpenshiftVersion(ocpReleaseImage, pullSecret string) (*models.OpenshiftVersion, error)
}

func NewHandler(log logrus.FieldLogger, releaseHandler oc.Release,
	versions Versions, openshiftVersions models.OpenshiftVersions,
	releaseImageMirror string) *handler {
	return &handler{
		versions:           versions,
		openshiftVersions:  openshiftVersions,
		releaseHandler:     releaseHandler,
		releaseImageMirror: releaseImageMirror,
		log:                log,
	}
}

var _ restapi.VersionsAPI = (*handler)(nil)

type handler struct {
	versions           Versions
	openshiftVersions  models.OpenshiftVersions
	releaseHandler     oc.Release
	releaseImageMirror string
	log                logrus.FieldLogger
}

func (h *handler) ListComponentVersions(ctx context.Context, params operations.ListComponentVersionsParams) middleware.Responder {
	versions := models.Versions{}
	for _, v := range []string{h.versions.SelfVersion, h.versions.AgentDockerImg, h.versions.InstallerImage, h.versions.ControllerImage} {
		versions[getRepositoryName(v)] = v
	}

	return operations.NewListComponentVersionsOK().WithPayload(
		&models.ListVersions{
			Versions:   versions,
			ReleaseTag: h.versions.ReleaseTag,
		})
}

func getRepositoryName(image string) string {
	image_name := strings.Split(image, ":")[0]
	parts := strings.Split(image_name, "/")
	return parts[len(parts)-1]
}

func (h *handler) ListSupportedOpenshiftVersions(ctx context.Context, params operations.ListSupportedOpenshiftVersionsParams) middleware.Responder {
	return operations.NewListSupportedOpenshiftVersionsOK().WithPayload(h.openshiftVersions)
}

func (h *handler) GetReleaseImage(openshiftVersion string) (pullSpec string, err error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return "", err
	}
	if !h.IsOpenshiftVersionSupported(versionKey) {
		return "", errors.Errorf("No release image for unsupported openshift version %s", versionKey)
	}

	if h.openshiftVersions[versionKey].ReleaseImage == nil {
		return "", errors.Errorf("Release image was missing for openshift version %s", versionKey)
	}

	return *h.openshiftVersions[versionKey].ReleaseImage, nil
}

func (h *handler) GetRHCOSImage(openshiftVersion string) (string, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return "", err
	}
	if !h.IsOpenshiftVersionSupported(versionKey) {
		return "", errors.Errorf("No rhcos image for unsupported openshift version %s", versionKey)
	}

	if h.openshiftVersions[versionKey].RhcosImage == nil {
		return "", errors.Errorf("RHCOS image was missing for openshift version %s", versionKey)
	}

	return *h.openshiftVersions[versionKey].RhcosImage, nil
}

func (h *handler) GetRHCOSVersion(openshiftVersion string) (string, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return "", err
	}
	if !h.IsOpenshiftVersionSupported(versionKey) {
		return "", errors.Errorf("No rhcos version for unsupported openshift version %s", versionKey)
	}

	if h.openshiftVersions[versionKey].RhcosVersion == nil {
		return "", errors.Errorf("RHCOS version was missing for openshift version %s", versionKey)
	}

	return *h.openshiftVersions[versionKey].RhcosVersion, nil
}

func (h *handler) IsOpenshiftVersionSupported(versionKey string) bool {
	if _, ok := h.openshiftVersions[versionKey]; !ok {
		return false
	}

	return true
}

// Should return release version (as fetched from 'oc adm release info')
func (h *handler) GetReleaseVersion(openshiftVersion string) (string, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return "", err
	}
	if !h.IsOpenshiftVersionSupported(versionKey) {
		return "", errors.Errorf("No release version for unsupported openshift version %s", versionKey)
	}

	if h.openshiftVersions[versionKey].ReleaseVersion == nil {
		return h.GetOCPSemVer(openshiftVersion)
	}

	return *h.openshiftVersions[versionKey].ReleaseVersion, nil
}

// Returns the OpenshiftVersion entity
func (h *handler) GetVersion(openshiftVersion string) (*models.OpenshiftVersion, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return nil, err
	}
	if !h.IsOpenshiftVersionSupported(versionKey) {
		return nil, errors.Errorf("No release version for unsupported openshift version %s", versionKey)
	}

	releaseVersion, err := h.GetReleaseVersion(openshiftVersion)
	if err != nil {
		return nil, err
	}
	version := h.openshiftVersions[versionKey]
	version.ReleaseVersion = &releaseVersion
	return &version, nil
}

// Returns version in major.minor format
func (h *handler) GetKey(openshiftVersion string) (string, error) {
	v, err := version.NewVersion(openshiftVersion)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d", v.Segments()[0], v.Segments()[1]), nil
}

// Returns version in major.minor.patch format
func (h *handler) GetOCPSemVer(openshiftVersion string) (string, error) {
	v, err := version.NewVersion(openshiftVersion)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d.%d", v.Segments()[0], v.Segments()[1], v.Segments()[2]), nil
}

func (h *handler) AddOpenshiftVersion(ocpReleaseImage, pullSecret string) (*models.OpenshiftVersion, error) {
	// Get openshift version from release image metadata (oc adm release info)
	ocpVersion, err := h.releaseHandler.GetOpenshiftVersion(h.log, ocpReleaseImage, h.releaseImageMirror, pullSecret)
	if err != nil {
		return nil, err
	}

	// Return if version is not speficied in OPENSHIFT_VERSIONS
	ocpVersionKey, err := h.GetKey(ocpVersion)
	if err != nil {
		return nil, err
	}
	versionFromCache, ok := h.openshiftVersions[ocpVersionKey]
	if !ok {
		return nil, errors.Errorf("OCP version is not specified in OPENSHIFT_VERSIONS: %s", ocpVersionKey)
	}

	// Get version in major.minor.patch format
	ocpSemVer, err := h.GetOCPSemVer(ocpVersion)
	if err != nil {
		return nil, err
	}

	// Get SupportLevel or default to 'custom'
	var supportLevel string
	if versionFromCache.SupportLevel != nil {
		supportLevel = *versionFromCache.SupportLevel
	}
	supportLevel = models.OpenshiftVersionSupportLevelCustom

	// Create OpenshiftVersion according to fetched data
	openshiftVersion := &models.OpenshiftVersion{
		DisplayName:    &ocpVersion,
		ReleaseImage:   &ocpReleaseImage,
		ReleaseVersion: &ocpSemVer,
		RhcosImage:     versionFromCache.RhcosImage,
		RhcosVersion:   versionFromCache.RhcosVersion,
		SupportLevel:   &supportLevel,
	}

	// Store in map
	h.openshiftVersions[ocpVersionKey] = *openshiftVersion
	h.log.Infof("Stored OCP version: %s", ocpSemVer)

	return openshiftVersion, nil
}
