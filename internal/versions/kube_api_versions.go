package versions

import (
	context "context"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/go-openapi/swag"
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

type kubeAPIVersionsHandler struct {
	mustGatherVersions MustGatherVersions
	releaseImages      models.ReleaseImages
	imagesLock         sync.Mutex
	sem                *semaphore.Weighted
	releaseHandler     oc.Release
	releaseImageMirror string
	log                logrus.FieldLogger
	kubeClient         client.Client
}

// // GetMustGatherImages retrieves the must-gather images for a specified OpenShift version and CPU architecture.
// If the configuration does not include a must-gather image for the given version and architecture,
// the function attempts to locate a matching release image. It then uses the 'oc' CLI tool to find and add the corresponding
// OCP must-gather image.
func (h *kubeAPIVersionsHandler) GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error) {
	return getMustGatherImages(
		h.log,
		openshiftVersion,
		cpuArchitecture,
		pullSecret,
		h.releaseImageMirror,
		h.mustGatherVersions,
		h.GetReleaseImage,
		h.releaseHandler,
		&h.imagesLock,
	)
}

// GetReleaseImage attempts to retrieve a release image that matches a specified OpenShift version and CPU architecture.
// It first tries to find the image in the local cache. If the image is not found in the cache and a Kubernetes client is available (KubeAPI mode),
// it then fetches all cluster image sets, updates the cache with these image sets, and attempts the cache lookup again.
func (h *kubeAPIVersionsHandler) GetReleaseImage(ctx context.Context, openshiftVersion, cpuArchitecture, pullSecret string) (*models.ReleaseImage, error) {
	cpuArchitecture = common.NormalizeCPUArchitecture(cpuArchitecture)
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

// GetReleaseImageByURL retrieves a release image based on its URL.
// The function searches through the cached release images and returns the matching image if found.
// If the image is not present in the cache, it attempts to add the image to the cache by fetching its details
// (including OpenShift version and CPU architecture) using the specified URL and 'oc' / 'skopeo' CLI tools.
func (h *kubeAPIVersionsHandler) GetReleaseImageByURL(ctx context.Context, url, pullSecret string) (*models.ReleaseImage, error) {
	for _, image := range h.releaseImages {
		if swag.StringValue(image.URL) == url {
			return image, nil
		}
	}

	return h.addReleaseImage(url, pullSecret)
}

// Finds a release image from the cache that matches the specified version and architecture in the following
// priority:
// 1. Exact CPU Architecture & OpenShift x.y.z version match
// 2. Exact CPU Architecture & OpenShift Major Minor (x.y) version match
// 3. Multi-arch CPU with specified CPU Architecture & OpenShift x.y.z version match
// 4. Multi-arch CPU with specified CPU Architecture & OpenShift Major Minor (x.y) version match
func (h *kubeAPIVersionsHandler) getReleaseImageFromCache(openshiftVersion, cpuArchitecture string) (*models.ReleaseImage, error) {
	if cpuArchitecture == "" {
		// Empty implies default CPU architecture
		cpuArchitecture = common.DefaultCPUArchitecture
	}

	// Filter Release images by specified CPU architecture.
	exactCPUArchReleaseImages := funk.Filter(h.releaseImages, func(releaseImage *models.ReleaseImage) bool {
		return swag.StringValue(releaseImage.CPUArchitecture) == cpuArchitecture
	})

	// Filter multi-arch Release images by containing the specified CPU architecture
	multiArchReleaseImages := funk.Filter(h.releaseImages, func(releaseImage *models.ReleaseImage) bool {
		return *releaseImage.CPUArchitecture == common.MultiCPUArchitecture &&
			funk.Contains(releaseImage.CPUArchitectures, cpuArchitecture)
	})

	if funk.IsEmpty(exactCPUArchReleaseImages) && funk.IsEmpty(multiArchReleaseImages) {
		return nil, errors.Errorf("The requested CPU architecture (%s) isn't specified in release images list", cpuArchitecture)
	}

	releaseImage := getReleaseImageByVersion(openshiftVersion, exactCPUArchReleaseImages.([]*models.ReleaseImage), h.log)
	if releaseImage == nil {
		// Find release image from multi-arch list if one doesn't exist in the exact CPU Arch list
		releaseImage = getReleaseImageByVersion(openshiftVersion, multiArchReleaseImages.([]*models.ReleaseImage), h.log)
	}

	if releaseImage != nil {
		h.log.Debugf("Found release image for openshift version '%s', CPU architecture '%s'", openshiftVersion, cpuArchitecture)
		return releaseImage, nil
	}

	h.log.Debugf("The requested release image for version (%s) and CPU architecture (%s) isn't specified in release images list",
		openshiftVersion, cpuArchitecture)
	return nil, errors.Errorf(
		"The requested release image for version (%s) and CPU architecture (%s) isn't specified in release images list",
		openshiftVersion, cpuArchitecture)
}

func getReleaseImageByVersion(desiredOpenshiftVersion string, releaseImages []*models.ReleaseImage, log logrus.FieldLogger) *models.ReleaseImage {
	// Search for specified x.y.z openshift version
	releaseImage := funk.Find(releaseImages, func(releaseImage *models.ReleaseImage) bool {
		return *releaseImage.OpenshiftVersion == desiredOpenshiftVersion
	})

	if releaseImage == nil {
		// Fallback to x.y version
		majorMinorVersion, err := common.GetMajorMinorVersion(desiredOpenshiftVersion)
		if err != nil {
			log.Debugf("error occurred while trying to get the major.minor version of '%s'", desiredOpenshiftVersion)
			return nil
		}

		releaseImage = funk.Find(releaseImages, func(releaseImage *models.ReleaseImage) bool {
			// Starting from OCP 4.12 multi-arch release images do not have "-multi" suffix
			// reported by the CVO, but we still offer the sufix internally to allow for explicit
			// selection or single- or multi-arch payload.
			version := strings.TrimSuffix(*releaseImage.OpenshiftVersion, "-multi")
			return version == *majorMinorVersion
		})
	}

	if releaseImage != nil {
		return releaseImage.(*models.ReleaseImage)
	}

	return nil
}

// ValidateReleaseImageForRHCOS validates whether for a specified RHCOS version we have an OCP
// version that can be used. This functions performs a very weak matching because RHCOS versions
// are very loosely coupled with the OpenShift versions what allows for a variety of mix&match.
func (h *kubeAPIVersionsHandler) ValidateReleaseImageForRHCOS(rhcosVersion, cpuArchitecture string) error {
	return validateReleaseImageForRHCOS(h.log, rhcosVersion, cpuArchitecture, h.releaseImages)
}

// addReleaseImage adds a new release image to the handler's cache based on the specified image URL and pull secret.
// It extracts the OpenShift version and CPU architecture(s) from the release image metadata using 'oc' / 'skopeo' CLI tools.
func (h *kubeAPIVersionsHandler) addReleaseImage(releaseImageUrl, pullSecret string) (*models.ReleaseImage, error) {
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
