package versions

import (
	context "context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	models "github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/leader"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
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
	for _, releaseImage := range releaseImages {
		if err := validateReleaseImage(releaseImage); err != nil {
			return nil, errors.Wrap(err, "error occurred while validating release images")
		}

		normalizeReleaseImageCPUArchitecture(releaseImage)
	}

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

func AddReleaseImagesToDBIfNeeded(
	db *gorm.DB,
	releaseImages models.ReleaseImages,
	lead leader.ElectorInterface,
	log logrus.FieldLogger,
	enableKubeAPI bool,
	releaseSourcesLiteral string,
) error {
	// If it is kubeAPI mode or release sources was specified (therefore OpenShift Release Syncer will run),
	// we should not add the release images to the DB.
	if enableKubeAPI || releaseSourcesLiteral != "" {
		return nil
	}

	return lead.RunWithLeader(context.Background(), func() error {
		tx := db.Begin()

		var count int64
		if err := tx.Model(&models.ReleaseImage{}).Count(&count).Error; err != nil {
			return errors.Wrapf(err, "error occurred while trying to count the initial amount of release images in the DB")
		}

		log.Debugf("Truncating all release_images table. '%d' records", count)
		if err := tx.Exec("TRUNCATE TABLE release_images").Error; err != nil {
			tx.Rollback()
			return errors.Wrapf(err, "error occurred while trying to delete the release images in the DB")
		}

		if len(releaseImages) > 0 {
			log.Debugf("Inserting configuration release images to the DB. '%d' records", len(releaseImages))
			if err := tx.Create(releaseImages).Error; err != nil {
				tx.Rollback()
				return errors.Wrapf(err, "error occurred while trying to insert the release images to the DB")
			}
		}

		if err := tx.Commit().Error; err != nil {
			tx.Rollback()
			return errors.Wrapf(err, "error occurred while commiting changes to the DB")
		}

		return nil
	})
}

func ParseIgnoredOpenshiftVersions(
	ignoredOpenshiftVersions *[]string,
	ignoredOpenshiftVersionsLiteral string,
	failOnError func(err error, msg string, args ...interface{}),
) {
	if ignoredOpenshiftVersionsLiteral != "" {
		failOnError(json.Unmarshal([]byte(ignoredOpenshiftVersionsLiteral), ignoredOpenshiftVersions),
			"Failed to parse IGNORED_OPENSHIFT_VERSIONS json %s", ignoredOpenshiftVersionsLiteral)
	}
}

func ParseReleaseImages(
	releaseImages *models.ReleaseImages,
	releaseImagesLiteral string,
	failOnError func(err error, msg string, args ...interface{}),
) {
	if releaseImagesLiteral == "" {
		// ReleaseImages is optional (not used by the operator)
		return
	} else {
		failOnError(json.Unmarshal([]byte(releaseImagesLiteral), releaseImages),
			"Failed to parse RELEASE_IMAGES json %s", releaseImagesLiteral)
	}

	// For backward compatibility with release images that lack the CPUArchitectures field.
	funk.ForEach(*releaseImages, func(releaseImage *models.ReleaseImage) {
		if releaseImage.CPUArchitectures == nil {
			releaseImage.CPUArchitectures = []string{*releaseImage.CPUArchitecture}
		}
	})
}

func normalizeReleaseImageCPUArchitecture(releaseImage *models.ReleaseImage) {
	// Normalize release.CPUArchitecture and release.CPUArchitectures
	// TODO: remove this block when AI starts using aarch64 instead of arm64
	if swag.StringValue(releaseImage.CPUArchitecture) == common.MultiCPUArchitecture || swag.StringValue(releaseImage.CPUArchitecture) == common.AARCH64CPUArchitecture {
		*releaseImage.CPUArchitecture = common.NormalizeCPUArchitecture(*releaseImage.CPUArchitecture)
		for i := 0; i < len(releaseImage.CPUArchitectures); i++ {
			releaseImage.CPUArchitectures[i] = common.NormalizeCPUArchitecture(releaseImage.CPUArchitectures[i])
		}
	}
}

// validateReleaseImage ensures no missing values in Release image.
func validateReleaseImage(releaseImage *models.ReleaseImage) error {
	// Release images are not mandatory (dynamically added in kube-api flow),
	// validating fields for those specified in list.
	missingValueTemplate := "Missing value in ReleaseImage for '%s' field"
	if swag.StringValue(releaseImage.CPUArchitecture) == "" {
		return errors.Errorf(fmt.Sprintf(missingValueTemplate, "cpu_architecture"))
	}

	if swag.StringValue(releaseImage.OpenshiftVersion) == "" {
		return errors.Errorf(fmt.Sprintf(missingValueTemplate, "openshift_version"))
	}
	if swag.StringValue(releaseImage.URL) == "" {
		return errors.Errorf(fmt.Sprintf(missingValueTemplate, "url"))
	}
	if swag.StringValue(releaseImage.Version) == "" {
		return errors.Errorf(fmt.Sprintf(missingValueTemplate, "version"))
	}

	// To validate CPU architecture enum
	return releaseImage.Validate(strfmt.Default)
}
