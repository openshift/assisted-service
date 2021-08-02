package versions

import (
	"context"
	"fmt"
	"reflect"

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
	GetReleaseImage(openshiftVersion, cpuArchitecture string) (string, error)
	GetReleaseVersion(openshiftVersion, cpuArchitecture string) (string, error)
	GetRHCOSImage(openshiftVersion, cpuArchitecture string) (string, error)
	GetRHCOSRootFS(openshiftVersion, cpuArchitecture string) (string, error)
	GetRHCOSVersion(openshiftVersion, cpuArchitecture string) (string, error)
	GetCPUArchitectures(openshiftVersion string) ([]string, error)
	GetKey(openshiftVersion string) (string, error)
	GetRelease(openshiftVersion, cpuArchitecture string) (string, string, error)
	IsOpenshiftVersionSupported(versionKey, cpuArchitecture string) bool
	AddOpenshiftVersion(ocpReleaseImage, pullSecret string) (*models.OpenshiftVersion, error)
}

const (
	DefaultCPUArchitecture = "x86_64"
)

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

func (h *handler) GetReleaseImage(openshiftVersion, cpuArchitecture string) (pullSpec string, err error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return "", err
	}
	if !h.IsOpenshiftVersionSupported(versionKey, cpuArchitecture) {
		return "", errors.Errorf("No release image for unsupported openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	if h.openshiftVersions[versionKey].Images != nil {
		if images, ok := h.openshiftVersions[versionKey].Images[cpuArchitecture]; ok {
			return *images.ReleaseImage, nil
		}
		return "", errors.Errorf("Release image was missing for openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	if h.openshiftVersions[versionKey].ReleaseImage == nil {
		return "", errors.Errorf("Release image was missing for openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	return *h.openshiftVersions[versionKey].ReleaseImage, nil
}

func (h *handler) GetRHCOSImage(openshiftVersion, cpuArchitecture string) (string, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return "", err
	}
	if !h.IsOpenshiftVersionSupported(versionKey, cpuArchitecture) {
		return "", errors.Errorf("No rhcos image for unsupported openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	if h.openshiftVersions[versionKey].Images != nil {
		if images, ok := h.openshiftVersions[versionKey].Images[cpuArchitecture]; ok {
			return *images.RhcosImage, nil
		}
		return "", errors.Errorf("RHCOS image was missing for openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	if h.openshiftVersions[versionKey].RhcosImage == nil {
		return "", errors.Errorf("RHCOS image was missing for openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	return *h.openshiftVersions[versionKey].RhcosImage, nil
}

func (h *handler) GetRHCOSRootFS(openshiftVersion, cpuArchitecture string) (string, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return "", err
	}
	if !h.IsOpenshiftVersionSupported(versionKey, cpuArchitecture) {
		return "", errors.Errorf("No rhcos rootfs for unsupported openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	if h.openshiftVersions[versionKey].Images != nil {
		if images, ok := h.openshiftVersions[versionKey].Images[cpuArchitecture]; ok {
			return *images.RhcosRootfs, nil
		}
		return "", errors.Errorf("RHCOS rootfs was missing for openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	if h.openshiftVersions[versionKey].RhcosRootfs == nil {
		return "", errors.Errorf("RHCOS rootfs was missing for openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	return *h.openshiftVersions[versionKey].RhcosRootfs, nil
}

func (h *handler) GetRHCOSVersion(openshiftVersion, cpuArchitecture string) (string, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return "", err
	}
	if !h.IsOpenshiftVersionSupported(versionKey, cpuArchitecture) {
		return "", errors.Errorf("No rhcos version for unsupported openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	if h.openshiftVersions[versionKey].Images != nil {
		if images, ok := h.openshiftVersions[versionKey].Images[cpuArchitecture]; ok {
			return *images.RhcosVersion, nil
		}
		return "", errors.Errorf("RHCOS version was missing for openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	if h.openshiftVersions[versionKey].RhcosVersion == nil {
		return "", errors.Errorf("RHCOS version was missing for openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	return *h.openshiftVersions[versionKey].RhcosVersion, nil
}

func (h *handler) IsOpenshiftVersionSupported(versionKey, cpuArchitecture string) bool {
	if _, ok := h.openshiftVersions[versionKey]; !ok {
		return false
	}

	if cpuArchitecture == DefaultCPUArchitecture {
		return true
	}

	// An empty CPU architecture implies default ('x86_64') for backwards compatibility
	if cpuArchitecture == "" {
		cpuArchitecture = DefaultCPUArchitecture
	}

	cpuArchitectures, err := h.GetCPUArchitectures(versionKey)
	if err != nil {
		return false
	}
	for _, architecture := range cpuArchitectures {
		if architecture == cpuArchitecture {
			return true
		}
	}

	return false
}

// Should return release version (as fetched from 'oc adm release info')
func (h *handler) GetReleaseVersion(openshiftVersion, cpuArchitecture string) (string, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return "", err
	}
	if !h.IsOpenshiftVersionSupported(versionKey, cpuArchitecture) {
		return "", errors.Errorf("No release version for unsupported openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	if h.openshiftVersions[versionKey].Images != nil {
		if images, ok := h.openshiftVersions[versionKey].Images[cpuArchitecture]; ok {
			return *images.ReleaseVersion, nil
		}
		return "", errors.Errorf("No release version for openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	if h.openshiftVersions[versionKey].ReleaseVersion == nil {
		return "", errors.Errorf("No release version for openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	return *h.openshiftVersions[versionKey].ReleaseVersion, nil
}

// Get CPU architecture supported by the specified openshift version
func (h *handler) GetCPUArchitectures(openshiftVersion string) ([]string, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return nil, err
	}
	if _, ok := h.openshiftVersions[versionKey]; !ok {
		return nil, errors.Errorf("No supported CPU architectures openshift version %s", versionKey)
	}

	var cpuArchitectures []string
	if h.openshiftVersions[versionKey].Images != nil {
		for key := range h.openshiftVersions[versionKey].Images {
			cpuArchitectures = append(cpuArchitectures, key)
		}
	} else {
		cpuArchitectures = append(cpuArchitectures, DefaultCPUArchitecture)
	}

	return cpuArchitectures, nil
}

// Returns the release image and release version
func (h *handler) GetRelease(openshiftVersion, cpuArchitecture string) (string, string, error) {
	versionKey, err := h.GetKey(openshiftVersion)
	if err != nil {
		return "", "", err
	}
	if !h.IsOpenshiftVersionSupported(versionKey, cpuArchitecture) {
		return "", "", errors.Errorf("No release version for unsupported openshift version %s, CPU architecture %s", versionKey, cpuArchitecture)
	}

	releaseImage, err := h.GetReleaseImage(openshiftVersion, cpuArchitecture)
	if err != nil {
		return "", "", err
	}

	releaseVersion, err := h.GetReleaseVersion(openshiftVersion, cpuArchitecture)
	if err != nil {
		return "", "", err
	}

	return releaseImage, releaseVersion, nil
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
		RhcosVersion:   versionFromCache.RhcosVersion,
		SupportLevel:   &supportLevel,
	}

	// Store in map
	h.openshiftVersions[ocpVersionKey] = *openshiftVersion
	h.log.Infof("Stored OCP version: %s", ocpReleaseVersion)

	return openshiftVersion, nil
}
