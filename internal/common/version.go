package common

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/hashicorp/go-version"
)

func CheckIfValidVersion(v string) error {
	_, err := version.NewVersion(v)
	return err
}

func GetVersionSegments(v string) ([]string, error) {
	if err := CheckIfValidVersion(v); err != nil {
		return nil, err
	}

	// We use strings manipulation instead of go-version manipulation
	// because for example go-version treats '4.14' == '4.14.0' (as it should in semantic versioning)
	// but we want to see difference
	coreVersion := strings.Split(v, "-")[0]
	return strings.Split(coreVersion, "."), nil
}

func createTwoVersions(version1, version2 string) (*version.Version, *version.Version, error) {
	v1, err := version.NewVersion(version1)
	if err != nil {
		return nil, nil, err
	}
	v2, err := version.NewVersion(version2)
	if err != nil {
		return nil, nil, err
	}
	return v1, v2, nil
}

func VersionGreaterOrEqual(version1, version2 string) (bool, error) {
	v1, v2, err := createTwoVersions(version1, version2)
	if err != nil {
		return false, err
	}
	return v1.GreaterThanOrEqual(v2), nil
}

// BaseVersionGreaterOrEqual compare Major, Minor and Patch
func BaseVersionGreaterOrEqual(version, versionMayGreaterThan string) (bool, error) {
	// return version >= versionMayGreaterThan
	version = strings.Split(version, "-")[0]
	versionMayGreaterThan = strings.Split(versionMayGreaterThan, "-")[0]

	return VersionGreaterOrEqual(versionMayGreaterThan, version)
}

func BaseVersionLessThan(version, versionMayLessThan string) (bool, error) {
	isGreaterOrEqual, err := BaseVersionGreaterOrEqual(version, versionMayLessThan)
	if err != nil {
		return false, err
	}
	return !isGreaterOrEqual, nil
}

// BaseVersionEqual Compare Major and Minor of 2 different versions
func BaseVersionEqual(version1, versionMayEqual string) (bool, error) {
	majorMinorVersion1, err := GetMajorMinorVersion(version1)
	if err != nil {
		return false, err
	}
	majorMinorVersionMayEqual, err := GetMajorMinorVersion(versionMayEqual)
	if err != nil {
		return false, err
	}

	return *majorMinorVersion1 == *majorMinorVersionMayEqual, nil
}

func IsVersionPreRelease(v string) (*bool, error) {
	semVersion, err := version.NewVersion(v)
	if err != nil {
		return nil, err
	}

	return swag.Bool(semVersion.Prerelease() != ""), nil
}

func GetVersionSegmentsLength(version string) (*int, error) {
	versionSegments, err := GetVersionSegments(version)
	if err != nil {
		return nil, err
	}

	return swag.Int(len(versionSegments)), nil
}

func GetMajorMinorVersion(version string) (*string, error) {
	versionSegments, err := GetVersionSegments(version)
	if err != nil {
		return nil, err
	}

	if len(versionSegments) < 2 {
		return nil, errors.New("invalid version")
	}

	versionStr := fmt.Sprintf("%s.%s", versionSegments[0], versionSegments[1])
	return &versionStr, nil
}

func GetMajorVersion(version string) (*string, error) {
	versionSegments, err := GetVersionSegments(version)
	if err != nil {
		return nil, err
	}

	return swag.String(versionSegments[0]), nil
}
