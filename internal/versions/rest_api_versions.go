package versions

import (
	context "context"
	"strings"
	"sync"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	models "github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

type restAPIVersionsHandler struct {
	log                      logrus.FieldLogger
	releaseHandler           oc.Release
	imagesLock               sync.Mutex
	mustGatherVersions       MustGatherVersions
	ignoredOpenshiftVersions []string
	db                       *gorm.DB
}

// GetReleaseImage retrieves a release image based on a specified OpenShift version and CPU architecture.
// If the provided OpenShift version includes a patch version, it attempts to retrieve an exact match for the release image if available.
// For OpenShift versions specified as major.minor, it fetches the latest matching release image.
// The function returns an error Returns an error for other formats of OpenShift version or if no matching image can be found.
func (h *restAPIVersionsHandler) GetReleaseImage(_ context.Context, openshiftVersion, cpuArchitecture, _ string) (*models.ReleaseImage, error) {
	cpuArchitecture = common.NormalizeCPUArchitecture(cpuArchitecture)
	// validations
	if err := validateCPUArchitecture(cpuArchitecture); err != nil {
		return nil, err
	}
	versionFormat := common.GetVersionFormat(openshiftVersion)
	if versionFormat == common.NoneVersion || versionFormat == common.MajorVersion {
		return nil,
			errors.Errorf(
				"invalid openshiftVersion '%s'. Expected format: 'major.minor' or 'major.minor.patch', optionally followed by a prerelease identifier",
				openshiftVersion,
			)
	}

	query := h.db.Session(&gorm.Session{}).Where(&models.ReleaseImage{})
	query = query.Where(h.db.Session(&gorm.Session{}).
		Where("? = Any(cpu_architectures)", cpuArchitecture).
		Or("cpu_architecture = ?", cpuArchitecture),
	)

	// Find the exact version
	if versionFormat == common.MajorMinorPatchVersion {
		query = query.Where("version = ?", openshiftVersion)
		var releaseImage models.ReleaseImage
		err := query.Take(&releaseImage).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errors.Errorf("no release image found for openshiftVersion: '%s' and CPU architecture '%s'", openshiftVersion, cpuArchitecture)
			}

			return nil, err
		}

		if err = assertNotIgnored(*releaseImage.Version, h.ignoredOpenshiftVersions); err != nil {
			return nil, err
		}

		return &releaseImage, err
	}

	// openshiftVersion must be major.minor, find the latest version matching

	query = query.Where("openshift_version = ?", openshiftVersion)
	var releaseImages models.ReleaseImages
	err := query.Find(&releaseImages).Error
	if err != nil {
		return nil, err
	}

	if len(releaseImages) == 0 {
		return nil, errors.Errorf("no release image found for openshiftVersion: '%s' and CPU architecture '%s'", openshiftVersion, cpuArchitecture)
	}

	return getLatestReleaseImage(releaseImages, h.ignoredOpenshiftVersions)
}

// GetReleaseImageByURL fetches a release image using the specified URL.
// It returns an error if no matching image is found.
func (h *restAPIVersionsHandler) GetReleaseImageByURL(_ context.Context, url, _ string) (*models.ReleaseImage, error) {
	var releaseImage models.ReleaseImage
	query := h.db.Session(&gorm.Session{})
	err := query.Model(&models.ReleaseImage{}).Where("url = ?", url).Take(&releaseImage).Error
	if err != nil {
		return nil, errors.Wrapf(err, "error occurred while trying to retrieve release image with url '%s' from the db", url)
	}

	if err := assertNotIgnored(*releaseImage.Version, h.ignoredOpenshiftVersions); err != nil {
		return nil, err
	}

	return &releaseImage, nil
}

// GetMustGatherImages retrieves the must-gather images for a specified OpenShift version and CPU architecture.
// If the configuration does not include a must-gather image for the given version and architecture,
// the function attempts to locate a matching release image. It then uses the 'oc' CLI tool to find and add the corresponding
// OCP must-gather image.
func (h *restAPIVersionsHandler) GetMustGatherImages(openshiftVersion, cpuArchitecture, pullSecret string) (MustGatherVersion, error) {
	if cpuArchitecture == common.AARCH64CPUArchitecture {
		cpuArchitecture = common.ARM64CPUArchitecture
	}

	return getMustGatherImages(
		h.log,
		openshiftVersion,
		cpuArchitecture,
		pullSecret,
		"",
		h.mustGatherVersions,
		h.GetReleaseImage,
		h.releaseHandler,
		&h.imagesLock,
	)
}

// ValidateReleaseImageForRHCOS validates whether for a specified RHCOS version we have an OCP
// version that can be used. This functions performs a very weak matching because RHCOS versions
// are very loosely coupled with the OpenShift versions what allows for a variety of mix&match.
func (h *restAPIVersionsHandler) ValidateReleaseImageForRHCOS(rhcosVersion, cpuArch string) error {
	var releaseImages models.ReleaseImages
	err := h.db.Model(&models.ReleaseImage{}).Find(&releaseImages).Error
	if err != nil {
		return errors.Wrapf(err, "error occurred while trying to retrieve release images from the DB")
	}

	return validateReleaseImageForRHCOS(h.log, rhcosVersion, cpuArch, releaseImages)
}

func validateCPUArchitecture(cpuArchitecture string) error {
	// Creating dummy release image to validate cpu architecture
	releaseImage := &models.ReleaseImage{
		OpenshiftVersion: swag.String("dummy-version"),
		CPUArchitecture:  &cpuArchitecture,
		CPUArchitectures: []string{cpuArchitecture},
		Version:          swag.String("dummy-version"),
		URL:              swag.String("dummy-url"),
	}

	return releaseImage.Validate(strfmt.Default)
}

// getLatestReleaseImage returns the latest release image among a list of release images not included in ignore list,
// or error if none of the release images match. The latest release image is considered the latest none beta release image,
// or if all matching release images are beta then just the latest.
func getLatestReleaseImage(releaseImages models.ReleaseImages, ignoredOpenshiftVersions []string) (*models.ReleaseImage, error) {
	var latestReleaseImage *models.ReleaseImage

	for _, releaseImage := range releaseImages {
		if err := assertNotIgnored(*releaseImage.Version, ignoredOpenshiftVersions); err != nil {
			continue
		}

		if latestReleaseImage == nil {
			latestReleaseImage = releaseImage
			continue
		}

		isLatest, err := common.VersionGreaterOrEqual(
			strings.TrimSuffix(*releaseImage.Version, "-multi"),
			strings.TrimSuffix(*latestReleaseImage.Version, "-multi"),
		)
		if err != nil {
			return nil, err
		}

		// none-beta > beta, later-beta > beta
		if latestReleaseImage.SupportLevel == models.OpenshiftVersionSupportLevelBeta {
			if isLatest || releaseImage.SupportLevel != models.OpenshiftVersionSupportLevelBeta {
				latestReleaseImage = releaseImage
			}
		} else { // non-beta-later > non-beta
			if isLatest && releaseImage.SupportLevel != models.OpenshiftVersionSupportLevelBeta {
				latestReleaseImage = releaseImage
			}
		}
	}

	if latestReleaseImage == nil {
		return nil, errors.New("no matching release image found")
	}

	return latestReleaseImage, nil
}

func assertNotIgnored(version string, ignoredOpenshiftVersions []string) error {
	version = strings.TrimSuffix(version, "-multi")
	majorMinorVersion, err := common.GetMajorMinorVersion(version)
	if err != nil {
		return err
	}

	if funk.Contains(ignoredOpenshiftVersions, *majorMinorVersion) || funk.Contains(ignoredOpenshiftVersions, version) {
		return errors.Errorf("version '%s' is ignored", version)
	}

	return nil
}
