package versions

import (
	"context"

	"github.com/go-openapi/runtime/middleware"
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

//go:generate mockgen -package versions -destination mock_versions.go . Handler
type Handler interface {
	restapi.VersionsAPI
	GetReleaseImage(openshiftVersion string) (string, error)
	GetRHCOSImage(openshiftVersion string) (string, error)
	IsOpenshiftVersionSupported(openshiftVersion string) bool
}

func NewHandler(log logrus.FieldLogger, releaseHandler oc.Release,
	versions Versions, openshiftVersions models.OpenshiftVersions,
	releaseImageOverride string, releaseImageMirror string) *handler {
	return &handler{
		versions:             versions,
		openshiftVersions:    openshiftVersions,
		releaseImageOverride: releaseImageOverride,
	}
}

var _ restapi.VersionsAPI = (*handler)(nil)

type handler struct {
	versions             Versions
	openshiftVersions    models.OpenshiftVersions
	releaseImageOverride string
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

func (h *handler) GetReleaseImage(openshiftVersion string) (pullSpec string, err error) {
	if h.releaseImageOverride != "" {
		return h.releaseImageOverride, nil
	}

	if !h.IsOpenshiftVersionSupported(openshiftVersion) {
		return "", errors.Errorf("No release image for openshift version %s", openshiftVersion)
	}

	return h.openshiftVersions[openshiftVersion].ReleaseImage, nil
}

func (h *handler) GetRHCOSImage(openshiftVersion string) (pullSpec string, err error) {
	if !h.IsOpenshiftVersionSupported(openshiftVersion) {
		return "", errors.Errorf("No rhcos image for openshift version %s", openshiftVersion)
	}

	return h.openshiftVersions[openshiftVersion].RhcosImage, nil
}

func (h *handler) IsOpenshiftVersionSupported(openshiftVersion string) bool {
	if _, ok := h.openshiftVersions[openshiftVersion]; !ok {
		return false
	}

	return true
}
