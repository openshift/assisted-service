package versions

import (
	context "context"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/go-openapi/swag"
	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"golang.org/x/sync/semaphore"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type MustGatherVersion map[string]string
type MustGatherVersions map[string]MustGatherVersion

//go:generate mockgen --build_flags=--mod=mod -package versions -destination mock_versions.go -self_package github.com/openshift/assisted-service/internal/versions . Handler
type Handler interface {
	GetReleaseImage(ctx context.Context, openshiftVersion, cpuArchitecture, pullSecret string) (*models.ReleaseImage, error)
	GetDefaultReleaseImage(cpuArchitecture string) (*models.ReleaseImage, error)
	GetReleaseImageByURL(ctx context.Context, url, pullSecret string) (*models.ReleaseImage, error)
	GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error)
	ValidateReleaseImageForRHCOS(rhcosVersion, cpuArch string) error
}

func NewHandler(log logrus.FieldLogger, releaseHandler oc.Release, releaseImages models.ReleaseImages,
	mustGatherVersions MustGatherVersions, releaseImageMirror string, kubeClient client.Client) (*handler, error) {

	h := &handler{
		mustGatherVersions: mustGatherVersions,
		releaseImages:      releaseImages,
		releaseHandler:     releaseHandler,
		releaseImageMirror: releaseImageMirror,
		log:                log,
		kubeClient:         kubeClient,
		sem:                semaphore.NewWeighted(30),
	}

	if err := h.validateVersions(); err != nil {
		return nil, err
	}

	return h, nil
}

type handler struct {
	mustGatherVersions MustGatherVersions
	releaseImages      models.ReleaseImages
	imagesLock         sync.Mutex
	sem                *semaphore.Weighted
	releaseHandler     oc.Release
	releaseImageMirror string
	log                logrus.FieldLogger
	kubeClient         client.Client
}

func (h *handler) GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error) {
	majMinorVersion, err := toMajorMinor(openshiftVersion)
	if err != nil {
		return nil, err
	}
	cacheKey := fmt.Sprintf("%s-%s", majMinorVersion, cpuArchitecture)

	if h.mustGatherVersions == nil {
		h.mustGatherVersions = make(MustGatherVersions)
	}
	if h.mustGatherVersions[cacheKey] == nil {
		h.mustGatherVersions[cacheKey] = make(MustGatherVersion)
	}

	//check if ocp must-gather image is already in the cache
	if h.mustGatherVersions[cacheKey]["ocp"] != "" {
		versions := h.mustGatherVersions[cacheKey]
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
	h.mustGatherVersions[cacheKey]["ocp"] = ocpMustGatherImage

	versions := h.mustGatherVersions[cacheKey]
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

	// The image doesn't exist in the cache.
	// Fetch all the cluster image sets, cache them, then search the cache again

	clusterImageSets := &hivev1.ClusterImageSetList{}
	if err := h.kubeClient.List(ctx, clusterImageSets); err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	for _, clusterImageSet := range clusterImageSets.Items {
		if err := h.sem.Acquire(ctx, 1); err != nil {
			// don't fail the entire function if this iteration fails to acquire the semaphore
			continue
		}
		wg.Add(1)
		go func(clusterImageSet hivev1.ClusterImageSet) {
			defer func() {
				wg.Done()
				h.sem.Release(1)
			}()
			existsInCache := false
			for _, releaseImage := range h.releaseImages {
				if releaseImage.URL != nil && *releaseImage.URL == clusterImageSet.Spec.ReleaseImage {
					existsInCache = true
					break
				}
			}
			if !existsInCache {
				_, err := h.addReleaseImage(clusterImageSet.Spec.ReleaseImage, pullSecret)
				if err != nil {
					h.log.WithError(err).Warnf("Failed to add release image %s", clusterImageSet.Spec.ReleaseImage)
				}
			}
		}(clusterImageSet)
	}
	wg.Wait()

	return h.getReleaseImageFromCache(openshiftVersion, cpuArchitecture)
}

func (h *handler) GetReleaseImageByURL(ctx context.Context, url, pullSecret string) (*models.ReleaseImage, error) {
	for _, image := range h.releaseImages {
		if swag.StringValue(image.URL) == url {
			return image, nil
		}
	}

	return h.addReleaseImage(url, pullSecret)
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
			// Starting from OCP 4.12 multi-arch release images do not have "-multi" suffix
			// reported by the CVO, but we still offer the sufix internally to allow for explicit
			// selection or single- or multi-arch payload.
			version := strings.TrimSuffix(*releaseImage.OpenshiftVersion, "-multi")
			return version == versionKey
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

func (h *handler) addReleaseImage(releaseImageUrl, pullSecret string) (*models.ReleaseImage, error) {
	// Get openshift version from release image metadata (oc adm release info)
	ocpReleaseVersion, err := h.releaseHandler.GetOpenshiftVersion(h.log, releaseImageUrl, "", pullSecret)
	if err != nil {
		return nil, err
	}
	h.log.Debugf("For release image %s detected version: %s", releaseImageUrl, ocpReleaseVersion)

	// Get CPU architecture from release image. For single-arch image the returned list will contain
	// as single entry with the architecture. For multi-arch image the list will contain all the architectures
	// that the image references to.
	cpuArchitectures, err := h.releaseHandler.GetReleaseArchitecture(h.log, releaseImageUrl, "", pullSecret)
	if err != nil {
		return nil, err
	}
	h.log.Debugf("For release image %s detected architecture: %s", releaseImageUrl, cpuArchitectures)

	var cpuArchitecture string
	if len(cpuArchitectures) == 1 {
		cpuArchitecture = cpuArchitectures[0]
	} else {
		cpuArchitecture = common.MultiCPUArchitecture
	}

	// lock for the rest of this function so we can call it concurrently
	h.imagesLock.Lock()
	defer h.imagesLock.Unlock()

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
		// Normalize release.CPUArchitecture and release.CPUArchitectures
		// TODO: remove this block when AI starts using aarch64 instead of arm64
		if swag.StringValue(release.CPUArchitecture) == common.MultiCPUArchitecture || swag.StringValue(release.CPUArchitecture) == common.AARCH64CPUArchitecture {
			*release.CPUArchitecture = common.NormalizeCPUArchitecture(*release.CPUArchitecture)
			for i := 0; i < len(release.CPUArchitectures); i++ {
				release.CPUArchitectures[i] = common.NormalizeCPUArchitecture(release.CPUArchitectures[i])
			}

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

func extractHost(destination string) string {
	patterns := []string{
		"^\\[([^\\]]+)\\]:\\d+$",
		"^([^:]+[.][^:]+):\\d+$",
	}
	for _, p := range patterns {
		r := regexp.MustCompile(p)
		if matches := r.FindStringSubmatch(destination); len(matches) == 2 {
			return matches[1]
		}
	}
	return destination
}

func GetReleaseImageHost(cluster *common.Cluster, versionHandler Handler) (string, error) {
	releaseImage, err := versionHandler.GetReleaseImage(context.Background(), cluster.OpenshiftVersion, cluster.CPUArchitecture, cluster.PullSecret)
	if err != nil {
		return "", err
	}
	splits := strings.Split(swag.StringValue(releaseImage.URL), "/")
	if len(splits) < 2 {
		return "", errors.Errorf("failed to get release image domain from %s", swag.StringValue(releaseImage.URL))
	}
	return extractHost(splits[0]), nil
}
