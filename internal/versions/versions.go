package versions

import (
	context "context"
	"fmt"
	"runtime/debug"

	"github.com/go-openapi/swag"
	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MustGatherVersion map[string]string
type MustGatherVersions map[string]MustGatherVersion

//go:generate mockgen --build_flags=--mod=mod -package versions -destination mock_versions.go -self_package github.com/openshift/assisted-service/internal/versions . Handler
type Handler interface {
	GetReleaseImage(ctx context.Context, openshiftVersion, cpuArchitecture, pullSecret string) (*models.ReleaseImage, error)
	GetDefaultReleaseImage(cpuArchitecture string) (*models.ReleaseImage, error)
	AddReleaseImage(releaseImageUrl, pullSecret, ocpReleaseVersion string, cpuArchitectures []string) (*models.ReleaseImage, error)
	GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error)
	ValidateReleaseImageForRHCOS(rhcosVersion, cpuArch string) error
}

func NewHandler(log logrus.FieldLogger, releaseHandler oc.Release, osImages OSImages, releaseImages models.ReleaseImages,
	mustGatherVersions MustGatherVersions, releaseImageMirror string, kubeClient client.Client) (*handler, error) {

	h := &handler{
		mustGatherVersions: mustGatherVersions,
		osImages:           osImages,
		releaseImages:      releaseImages,
		releaseHandler:     releaseHandler,
		releaseImageMirror: releaseImageMirror,
		log:                log,
		kubeClient:         kubeClient,
	}

	if err := h.validateVersions(); err != nil {
		return nil, err
	}

	return h, nil
}

type handler struct {
	mustGatherVersions MustGatherVersions
	osImages           OSImages
	releaseImages      models.ReleaseImages
	releaseHandler     oc.Release
	releaseImageMirror string
	log                logrus.FieldLogger
	kubeClient         client.Client
}

func (h *handler) GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error) {
	versionKey, err := toMajorMinor(openshiftVersion)
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
	releaseImage, err := h.GetReleaseImage(context.Background(), openshiftVersion, cpuArchitecture, pullSecret)
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

func (h *handler) GetReleaseImage(ctx context.Context, openshiftVersion, cpuArchitecture, pullSecret string) (*models.ReleaseImage, error) {
	image, err := h.getReleaseImageFromCache(openshiftVersion, cpuArchitecture)
	if err == nil || h.kubeClient == nil {
		return image, err
	}

	clusterImageSets := &hivev1.ClusterImageSetList{}
	if err := h.kubeClient.List(ctx, clusterImageSets); err != nil {
		return nil, err
	}
	for _, clusterImageSet := range clusterImageSets.Items {
		existsInCache := false
		for _, releaseImage := range h.releaseImages {
			if releaseImage.URL != nil && *releaseImage.URL == clusterImageSet.Spec.ReleaseImage {
				existsInCache = true
				break
			}
		}
		if !existsInCache {
			_, err := h.AddReleaseImage(clusterImageSet.Spec.ReleaseImage, pullSecret, "", nil)
			if err != nil {
				h.log.WithError(err).Warnf("Failed to add release image %s", clusterImageSet.Spec.ReleaseImage)
			}
		}
	}

	return h.getReleaseImageFromCache(openshiftVersion, cpuArchitecture)
}

func (h *handler) getReleaseImageFromCache(openshiftVersion, cpuArchitecture string) (*models.ReleaseImage, error) {
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
		versionKey, err := toMajorMinor(openshiftVersion)
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
	rhcosVersion, err := toMajorMinor(rhcosVersion)
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
				minorVersion, err := toMajorMinor(*releaseImage.OpenshiftVersion)
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
		osImage, err = h.osImages.GetOsImage(ocpReleaseVersion, cpuArchitecture)
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

// Returns version in major.minor format
func toMajorMinor(openshiftVersion string) (string, error) {
	v, err := version.NewVersion(openshiftVersion)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d", v.Segments()[0], v.Segments()[1]), nil
}

// Ensure no missing values in Release images.
func (h *handler) validateVersions() error {
	// Release images are not mandatory (dynamically added in kube-api flow),
	// validating fields for those specified in list.
	missingValueTemplate := "Missing value in ReleaseImage for '%s' field"
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

// GetRevision returns the overall codebase version. It's for detecting
// what code a binary was built from.
func GetRevision() string {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return "<unknown>"
	}

	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return "<unknown>"
}
