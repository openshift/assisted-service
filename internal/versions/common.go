package versions

import (
	context "context"
	"fmt"
	"sync"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	models "github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
	"gorm.io/gorm"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen --build_flags=--mod=mod -package versions -destination mock_versions.go -self_package github.com/openshift/assisted-service/internal/versions . Handler
type Handler interface {
	GetReleaseImage(ctx context.Context, openshiftVersion, cpuArchitecture, pullSecret string) (*models.ReleaseImage, error)
	GetReleaseImageByURL(ctx context.Context, url, pullSecret string) (*models.ReleaseImage, error)
	GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error)
	ValidateReleaseImageForRHCOS(rhcosVersion, cpuArch string) error
}

type MustGatherVersion map[string]string
type MustGatherVersions map[string]MustGatherVersion

func NewHandler(
	log logrus.FieldLogger,
	releaseHandler oc.Release,
	releaseImages models.ReleaseImages,
	mustGatherVersions MustGatherVersions,
	releaseImageMirror string,
	kubeClient client.Client,
	ignoredOpenshiftVersions []string,
	db *gorm.DB,
	enableKubeAPI bool,
	releaseSources models.ReleaseSources,
) (Handler, error) {
	if enableKubeAPI {
		h := &kubeAPIVersionsHandler{
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

	restHandler := &restAPIVersionsHandler{
		log:                      log,
		releaseHandler:           releaseHandler,
		mustGatherVersions:       mustGatherVersions,
		ignoredOpenshiftVersions: ignoredOpenshiftVersions,
		db:                       db,
	}

	return restHandler, nil
}

func getMustGatherImages(
	log logrus.FieldLogger,
	openshiftVersion,
	cpuArchitecture,
	pullSecret,
	releaseImageMirror string,
	mustGatherVersions MustGatherVersions,
	getReleaseImage func(ctx context.Context, openshiftVersion, cpuArchitecture, pullSecret string) (*models.ReleaseImage, error),
	releaseHandler oc.Release,
	imagesLock *sync.Mutex,
) (MustGatherVersion, error) {
	imagesLock.Lock()
	defer imagesLock.Unlock()

	majMinorVersion, err := common.GetMajorMinorVersion(openshiftVersion)
	if err != nil {
		return nil, err
	}
	cacheKey := fmt.Sprintf("%s-%s", *majMinorVersion, cpuArchitecture)

	if mustGatherVersions == nil {
		mustGatherVersions = make(MustGatherVersions)
	}
	if mustGatherVersions[cacheKey] == nil {
		mustGatherVersions[cacheKey] = make(MustGatherVersion)
	}

	//check if ocp must-gather image is already in the cache
	if mustGatherVersions[cacheKey]["ocp"] != "" {
		versions := mustGatherVersions[cacheKey]
		return versions, nil
	}
	//if not, fetch it from the release image and add it to the cache
	releaseImage, err := getReleaseImage(context.Background(), openshiftVersion, cpuArchitecture, pullSecret)
	if err != nil {
		return nil, err
	}
	ocpMustGatherImage, err := releaseHandler.GetMustGatherImage(log, *releaseImage.URL, releaseImageMirror, pullSecret)
	if err != nil {
		return nil, err
	}
	mustGatherVersions[cacheKey]["ocp"] = ocpMustGatherImage

	versions := mustGatherVersions[cacheKey]
	return versions, nil
}

func validateReleaseImageForRHCOS(
	log logrus.FieldLogger, rhcosVersion, cpuArchitecture string, releaseImages models.ReleaseImages,
) error {
	// Multi is not a valid RHCOS CPU architecture, its sub-architectures are
	if cpuArchitecture == common.MultiCPUArchitecture {
		return errors.Errorf("The requested RHCOS version (%s, arch: %s) does not have a matching OpenShift release image", rhcosVersion, cpuArchitecture)
	}

	rhcosVersionPtr, err := common.GetMajorMinorVersion(rhcosVersion)
	if err != nil {
		return err
	}

	if cpuArchitecture == "" {
		// Empty implies default CPU architecture
		cpuArchitecture = common.DefaultCPUArchitecture
	}

	for _, releaseImage := range releaseImages {
		minorVersion, err := common.GetMajorMinorVersion(*releaseImage.OpenshiftVersion)
		if err != nil {
			return err
		}

		if cpuArchitecture == *releaseImage.CPUArchitecture && *minorVersion == *rhcosVersionPtr {
			log.Debugf("Validator for the architecture %s found the following OCP version: %s", cpuArchitecture, *releaseImage.Version)
			return nil
		}

		for _, arch := range releaseImage.CPUArchitectures {
			if arch == cpuArchitecture && *minorVersion == *rhcosVersionPtr {
				if *minorVersion == *rhcosVersionPtr {
					log.Debugf("Validator for the architecture %s found the following OCP version: %s", cpuArchitecture, *releaseImage.Version)
					return nil
				}
			}
		}
	}

	return errors.Errorf("The requested RHCOS version (%s, arch: %s) does not have a matching OpenShift release image", *rhcosVersionPtr, cpuArchitecture)
}
