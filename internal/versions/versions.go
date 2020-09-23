package versions

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/versions"
)

type Versions struct {
	SelfVersion       string `envconfig:"SELF_VERSION" default:"quay.io/ocpmetal/assisted-iso-create:latest"`
	ImageBuilder      string `envconfig:"IMAGE_BUILDER" default:"quay.io/ocpmetal/assisted-iso-create:latest"`
	AgentDockerImg    string `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/ocpmetal/agent:latest"`
	IgnitionGenerator string `envconfig:"IGNITION_GENERATE_IMAGE" default:"quay.io/ocpmetal/assisted-ignition-generator:latest"`
	InstallerImage    string `envconfig:"INSTALLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer:latest"`
	ControllerImage   string `envconfig:"CONTROLLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer-controller:latest"`
	ReleaseTag        string `envconfig:"RELEASE_TAG" default:""`
}

func NewHandler(versions Versions, log logrus.FieldLogger, openshiftVersions []string) *handler {
	return &handler{versions: versions, log: log, openshiftVersions: openshiftVersions}
}

var _ restapi.VersionsAPI = (*handler)(nil)

type handler struct {
	versions          Versions
	openshiftVersions []string
	log               logrus.FieldLogger
}

func (h *handler) ListComponentVersions(ctx context.Context, params operations.ListComponentVersionsParams) middleware.Responder {
	return operations.NewListComponentVersionsOK().WithPayload(
		&models.ListVersions{
			Versions: models.Versions{
				"assisted-installer-service":    h.versions.SelfVersion,
				"image-builder":                 h.versions.ImageBuilder,
				"discovery-agent":               h.versions.AgentDockerImg,
				"assisted-ignition-generator":   h.versions.IgnitionGenerator,
				"assisted-installer":            h.versions.InstallerImage,
				"assisted-installer-controller": h.versions.ControllerImage,
			},
			ReleaseTag: h.versions.ReleaseTag,
		})
}

func (h *handler) ListSupportedOpenshiftVersions(ctx context.Context, params operations.ListSupportedOpenshiftVersionsParams) middleware.Responder {
	return operations.NewListSupportedOpenshiftVersionsOK().WithPayload(h.openshiftVersions)
}
