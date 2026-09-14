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
	GetOsImage(openshiftVersion, cpuArchitecture, osStream, infraImageType string) (*models.OsImage, error)
	GetLatestOsImage(cpuArchitecture, osStream, infraImageType string) (*models.OsImage, error)
	GetOsImageOrLatest(version, cpuArch, osStream, infraImageType string) (*models.OsImage, error)
	GetCPUArchitectures(openshiftVersion string) []string
	GetOpenshiftVersions() []string
}

type osImageList models.OsImages

func NewOSImages(images models.OsImages, enableImageService bool) (OSImages, error) {
	if len(images) == 0 && enableImageService {
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
		return errors.Errorf(missingValueTemplate, "url", *osImage.OpenshiftVersion)
	}
	if swag.StringValue(osImage.Version) == "" {
		return errors.Errorf(missingValueTemplate, "version", *osImage.OpenshiftVersion)
	}
	if osImage.CPUArchitecture == nil {
		return errors.Errorf("osImage version '%s' CPU architecture is missing", *osImage.OpenshiftVersion)
	}
	if err := osImage.Validate(strfmt.Default); err != nil {
		return errors.Wrap(err, fmt.Sprintf("osImage version '%s' CPU architecture is not valid", *osImage.OpenshiftVersion))
	}

	return nil
}

// Returns the OsImage entity matching the given OpenShift version, CPU architecture and optional OS stream.
func (images osImageList) GetOsImage(openshiftVersion, cpuArchitecture, osStream, osImageType string) (*models.OsImage, error) {
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
	}).([]*models.OsImage)
	if funk.IsEmpty(archImages) {
		return nil, errors.Errorf("The requested CPU architecture (%s) isn't specified in OS images list", cpuArchitecture)
	}

	filteredByImageType := filterByInfraImageType(archImages, osImageType)
	candidates := findVersionCandidates(filteredByImageType, openshiftVersion)
	if len(candidates) == 0 {
		return nil, errors.Errorf(
			"The requested OS image for version (%s) and CPU architecture (%s) isn't specified in OS images list",
			openshiftVersion, cpuArchitecture)
	}

	return selectByOsStream(candidates, osStream, openshiftVersion, cpuArchitecture)
}

func findVersionCandidates(archImages []*models.OsImage, openshiftVersion string) []*models.OsImage {
	// Search for specified x.y.z openshift version
	exact := funk.Filter(archImages, func(osImage *models.OsImage) bool {
		return swag.StringValue(osImage.OpenshiftVersion) == openshiftVersion
	}).([]*models.OsImage)
	if len(exact) > 0 {
		return exact
	}

	versionKey, err := common.GetMajorMinorVersion(openshiftVersion)
	if err != nil {
		return nil
	}

	// Fallback to x.y version
	majorMinor := funk.Filter(archImages, func(osImage *models.OsImage) bool {
		return *osImage.OpenshiftVersion == *versionKey
	}).([]*models.OsImage)
	if len(majorMinor) > 0 {
		return majorMinor
	}

	// Find latest available patch version by x.y version
	patchMatches := funk.Filter(archImages, func(osImage *models.OsImage) bool {
		imageVersionKey, err := common.GetMajorMinorVersion(*osImage.OpenshiftVersion)
		if err != nil {
			return false
		}
		return *imageVersionKey == *versionKey
	}).([]*models.OsImage)
	if len(patchMatches) == 0 {
		return nil
	}

	sort.Slice(patchMatches, func(i, j int) bool {
		v1, _ := version.NewVersion(*patchMatches[i].OpenshiftVersion)
		v2, _ := version.NewVersion(*patchMatches[j].OpenshiftVersion)
		return v1.GreaterThan(v2)
	})

	latestVersion := *patchMatches[0].OpenshiftVersion
	return funk.Filter(patchMatches, func(osImage *models.OsImage) bool {
		return *osImage.OpenshiftVersion == latestVersion
	}).([]*models.OsImage)
}

func filterByInfraImageType(images []*models.OsImage, infraImageType string) []*models.OsImage {
	return funk.Filter(images, func(img *models.OsImage) bool {
		return imageEntryMatchesInfraImageType(img.Type, infraImageType)
	}).([]*models.OsImage)
}

func imageEntryMatchesInfraImageType(imageEntryType string, infraImageType string) bool {
	if infraImageType == string(models.ImageTypeDisconnectedIso) {
		// Disconnected InfraEnv → only disconnected OS image entries
		return imageEntryType == string(models.ImageTypeDisconnectedIso)
	}
	// Online InfraEnv → OS image entries with empty type only
	return imageEntryType != string(models.ImageTypeDisconnectedIso)
}

func selectByOsStream(candidates []*models.OsImage, osStream, openshiftVersion, cpuArchitecture string) (*models.OsImage, error) {
	if osStream != "" {
		match := funk.Find(candidates, func(osImage *models.OsImage) bool {
			return swag.StringValue(osImage.OsStream) == osStream
		})
		if match == nil {
			return nil, errors.Errorf(
				"The requested OS image for version (%s), CPU architecture (%s) and OS stream (%s) isn't specified in OS images list",
				openshiftVersion, cpuArchitecture, osStream)
		}
		return match.(*models.OsImage), nil
	}

	hasOsStreamMetadata := false
	for _, c := range candidates {
		if swag.StringValue(c.OsStream) != "" || swag.BoolValue(c.DefaultOsStream) {
			hasOsStreamMetadata = true
			break
		}
	}

	// Legacy catalog with no stream metadata: keep first-match behavior
	if !hasOsStreamMetadata {
		return candidates[0], nil
	}

	defaults := funk.Filter(candidates, func(osImage *models.OsImage) bool {
		return swag.BoolValue(osImage.DefaultOsStream)
	}).([]*models.OsImage)

	switch len(defaults) {
	case 1:
		return defaults[0], nil
	case 0:
		return nil, errors.Errorf(
			"No default OS stream found for version (%s) and CPU architecture (%s)",
			openshiftVersion, cpuArchitecture)
	default:
		return nil, errors.Errorf(
			"Multiple default OS streams found for version (%s) and CPU architecture (%s)",
			openshiftVersion, cpuArchitecture)
	}
}

// Returns the latest OSImage entity for a specified CPU architecture and optional OS stream
func (images osImageList) GetLatestOsImage(cpuArchitecture, osStream, infraImageType string) (*models.OsImage, error) {
	var latest *models.OsImage
	openshiftVersions := images.GetOpenshiftVersions()
	for _, k := range openshiftVersions {
		osImage, err := images.GetOsImage(k, cpuArchitecture, osStream, infraImageType)
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

func (images osImageList) GetOsImageOrLatest(version, cpuArch, osStream, imageType string) (*models.OsImage, error) {
	var osImage *models.OsImage
	var err error
	if version != "" {
		osImage, err = images.GetOsImage(version, cpuArch, osStream, imageType)
		if err != nil {
			return nil, errors.Wrapf(err, "No OS image for Openshift version (%s), CPU architecture (%s) and OS stream (%s)", version, cpuArch, osStream)
		}
	} else {
		osImage, err = images.GetLatestOsImage(cpuArch, osStream, imageType)
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to get latest OS image for CPU architecture (%s) and OS stream (%s)", cpuArch, osStream)
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
