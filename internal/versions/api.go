package versions

import (
	context "context"
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
	versionsHandler *handler
	osImages        OSImages
}

var _ restapi.VersionsAPI = (*apiHandler)(nil)

func NewAPIHandler(log logrus.FieldLogger, versions Versions, authzHandler auth.Authorizer, versionsHandler *handler, osImages OSImages) restapi.VersionsAPI {
	return &apiHandler{
		authzHandler:    authzHandler,
		versions:        versions,
		log:             log,
		versionsHandler: versionsHandler,
		osImages:        osImages,
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

func (h *apiHandler) V2ListSupportedOpenshiftVersions(ctx context.Context, params operations.V2ListSupportedOpenshiftVersionsParams) middleware.Responder {
	openshiftVersions := models.OpenshiftVersions{}
	hasMultiarchAuthorization := false
	checkedForMultiarchAuthorization := false

	for _, releaseImage := range h.versionsHandler.releaseImages {
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

		for _, arch := range supportedArchs {
			key := *releaseImage.OpenshiftVersion
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

			// In order to handle multi-arch release images correctly in the UI, we need to tune
			// their DisplayName to contain an appropriate suffix. If the suffix comes from the
			// JSON definition we do nothing, but in case it's missing there (because CVO is not
			// reporting it), we add it ourselves.
			displayName := *releaseImage.Version
			if len(supportedArchs) > 1 && !strings.HasSuffix(displayName, "-multi") {
				displayName = displayName + "-multi"
			}

			openshiftVersion, exists := openshiftVersions[key]
			if !exists {
				openshiftVersion = models.OpenshiftVersion{
					CPUArchitectures: []string{arch},
					Default:          releaseImage.Default,
					DisplayName:      swag.String(displayName),
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
