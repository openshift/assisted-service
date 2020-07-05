package versions

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/bm-inventory/restapi"
	operations "github.com/filanov/bm-inventory/restapi/operations/versions"
	"github.com/go-openapi/runtime/middleware"
)

type Versions struct {
	SelfVersion         string `envconfig:"SELF_VERSION" default:"quay.io/ocpmetal/installer-image-build:stable"`
	ImageBuilder        string `envconfig:"IMAGE_BUILDER" default:"quay.io/ocpmetal/installer-image-build:stable"`
	AgentDockerImg      string `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/ocpmetal/agent:stable"`
	KubeconfigGenerator string `envconfig:"KUBECONFIG_GENERATE_IMAGE" default:"quay.io/ocpmetal/ignition-manifests-and-kubeconfig-generate:stable"`
	InstallerImage      string `envconfig:"INSTALLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer:stable"`
	ReleaseTag          string `envconfig:"RELEASE_TAG" default:""`
}

func NewHandler(versions Versions) *handler {
	return &handler{versions: versions}
}

var _ restapi.VersionsAPI = (*handler)(nil)

type handler struct {
	versions Versions
}

func (h *handler) ListComponentVersions(ctx context.Context, params operations.ListComponentVersionsParams) middleware.Responder {
	return operations.NewListComponentVersionsOK().WithPayload(
		&models.ListVersions{
			Versions: models.Versions{
				"assisted-installer-service":                 h.versions.SelfVersion,
				"image-builder":                              h.versions.ImageBuilder,
				"discovery-agent":                            h.versions.AgentDockerImg,
				"ignition-manifests-and-kubeconfig-generate": h.versions.KubeconfigGenerator,
				"assisted-installer":                         h.versions.InstallerImage,
			},
			ReleaseTag: h.versions.ReleaseTag,
		})
}
