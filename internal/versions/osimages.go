package versions

import (
	"fmt"
	"sort"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

//go:generate mockgen --build_flags=--mod=mod -package versions -destination mock_osimages.go -self_package github.com/openshift/assisted-service/internal/versions . OSImages
type OSImages interface {
	GetOsImage(openshiftVersion, cpuArchitecture string) (*models.OsImage, error)
	GetLatestOsImage(cpuArchitecture string) (*models.OsImage, error)
	GetOsImageOrLatest(version string, cpuArch string) (*models.OsImage, error)
	GetCPUArchitectures(openshiftVersion string) []string
	GetOpenshiftVersions() []string
}

type osImageList models.OsImages

func NewOSImages(images models.OsImages) (OSImages, error) {
	if len(images) == 0 {
		return nil, errors.New("No OS images provided")
	}
	for _, osImage := range images {
		if err := validateOSImage(osImage); err != nil {
			return nil, err
		}

		normalizeOSImageCPUArchitecture(osImage)
	}

	return osImageList(images), nil
}

func validateOSImage(osImage *models.OsImage) error {
	missingValueTemplate := "Missing value in OSImage for '%s' field (openshift_version: %s)"
	if swag.StringValue(osImage.OpenshiftVersion) == "" {
		return errors.Errorf("Missing openshift_version in OsImage: %v", osImage)
	}

	if swag.StringValue(osImage.URL) == "" {
		return errors.Errorf(fmt.Sprintf(missingValueTemplate, "url", *osImage.OpenshiftVersion))
	}
	if swag.StringValue(osImage.Version) == "" {
		return errors.Errorf(fmt.Sprintf(missingValueTemplate, "version", *osImage.OpenshiftVersion))
	}
	if osImage.CPUArchitecture == nil {
		return errors.Errorf("osImage version '%s' CPU architecture is missing", *osImage.OpenshiftVersion)
	}
	if err := osImage.Validate(strfmt.Default); err != nil {
		return errors.Wrapf(err, "osImage version '%s' CPU architecture is not valid", *osImage.OpenshiftVersion)
	}

	return nil
}

// Returns the OsImage entity
func (images osImageList) GetOsImage(openshiftVersion, cpuArchitecture string) (*models.OsImage, error) {
	cpuArchitecture = common.NormalizeCPUArchitecture(cpuArchitecture)

	if cpuArchitecture == "" {
		// Empty implies default CPU architecture
		cpuArchitecture = common.DefaultCPUArchitecture
	}

	// Filter OS images by specified CPU architecture
	archImages := funk.Filter(images, func(osImage *models.OsImage) bool {
		if swag.StringValue(osImage.CPUArchitecture) == "" {
			return cpuArchitecture == common.DefaultCPUArchitecture
		}
		return swag.StringValue(osImage.CPUArchitecture) == cpuArchitecture
	})
	if funk.IsEmpty(archImages) {
		return nil, errors.Errorf("The requested CPU architecture (%s) isn't specified in OS images list", cpuArchitecture)
	}

	// Search for specified x.y.z openshift version
	osImage := funk.Find(archImages, func(osImage *models.OsImage) bool {
		return swag.StringValue(osImage.OpenshiftVersion) == openshiftVersion
	})

	versionKey, err := common.GetMajorMinorVersion(openshiftVersion)
	if err != nil {
		return nil, err
	}

	if osImage == nil {
		// Fallback to x.y version
		osImage = funk.Find(archImages, func(osImage *models.OsImage) bool {
			return *osImage.OpenshiftVersion == *versionKey
		})
	}

	if osImage == nil {
		// Find latest available patch version by x.y version
		osImages := funk.Filter(archImages, func(osImage *models.OsImage) bool {
			imageVersionKey, err := common.GetMajorMinorVersion(*osImage.OpenshiftVersion)
			if err != nil {
				return false
			}
			return *imageVersionKey == *versionKey
		}).([]*models.OsImage)
		sort.Slice(osImages, func(i, j int) bool {
			v1, _ := version.NewVersion(*osImages[i].OpenshiftVersion)
			v2, _ := version.NewVersion(*osImages[j].OpenshiftVersion)
			return v1.GreaterThan(v2)
		})
		if !funk.IsEmpty(osImages) {
			osImage = osImages[0]
		}
	}

	if osImage != nil {
		return osImage.(*models.OsImage), nil
	}

	return nil, errors.Errorf(
		"The requested OS image for version (%s) and CPU architecture (%s) isn't specified in OS images list",
		openshiftVersion, cpuArchitecture)
}

// Returns the latest OSImage entity for a specified CPU architecture
func (images osImageList) GetLatestOsImage(cpuArchitecture string) (*models.OsImage, error) {
	var latest *models.OsImage
	openshiftVersions := images.GetOpenshiftVersions()
	for _, k := range openshiftVersions {
		osImage, err := images.GetOsImage(k, cpuArchitecture)
		if err != nil {
			continue
		}
		if latest == nil {
			latest = osImage
		} else {
			imageVer, _ := version.NewVersion(*osImage.OpenshiftVersion)
			latestVer, _ := version.NewVersion(*latest.OpenshiftVersion)
			if imageVer.GreaterThan(latestVer) {
				latest = osImage
			}
		}
	}
	if latest == nil {
		return nil, errors.Errorf("No OS images are available")
	}
	return latest, nil
}

func (images osImageList) GetOsImageOrLatest(version string, cpuArch string) (*models.OsImage, error) {
	var osImage *models.OsImage
	var err error
	if version != "" {
		osImage, err = images.GetOsImage(version, cpuArch)
		if err != nil {
			return nil, errors.Wrapf(err, "No OS image for Openshift version %s and architecture %s", version, cpuArch)
		}
	} else {
		osImage, err = images.GetLatestOsImage(cpuArch)
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to get latest OS image for architecture %s", cpuArch)
		}
	}
	return osImage, nil
}

// Get CPU architectures available for the specified openshift version
// according to the OS images list.
func (images osImageList) GetCPUArchitectures(openshiftVersion string) []string {
	cpuArchitectures := []string{}
	versionKey, err := common.GetMajorMinorVersion(openshiftVersion)
	if err != nil {
		return cpuArchitectures
	}
	for _, osImage := range images {
		if *osImage.OpenshiftVersion == openshiftVersion || *osImage.OpenshiftVersion == *versionKey {
			if swag.StringValue(osImage.CPUArchitecture) == "" {
				// Empty or missing property implies default CPU architecture
				defaultArch := common.DefaultCPUArchitecture
				osImage.CPUArchitecture = &defaultArch
			}
			if !funk.Contains(cpuArchitectures, *osImage.CPUArchitecture) {
				cpuArchitectures = append(cpuArchitectures, *osImage.CPUArchitecture)
			}
		}
	}
	return cpuArchitectures
}

// Get available openshift versions according to OS images list.
func (images osImageList) GetOpenshiftVersions() []string {
	versions := []string{}
	for _, image := range images {
		if !funk.Contains(versions, *image.OpenshiftVersion) {
			versions = append(versions, *image.OpenshiftVersion)
		}
	}
	return versions
}

func normalizeOSImageCPUArchitecture(osImage *models.OsImage) {
	// Normalize osImage.CPUArchitecture
	// TODO: remove this block when AI starts using aarch64 instead of arm64
	if *osImage.CPUArchitecture == common.AARCH64CPUArchitecture {
		*osImage.CPUArchitecture = common.NormalizeCPUArchitecture(*osImage.CPUArchitecture)
	}
}
