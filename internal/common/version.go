package common

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/go-version"
)

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

func GetMajorMinorVersion(version string) (*string, error) {
	version = strings.Split(version, "-")[0]
	splittedVersion := strings.Split(version, ".")

	if len(splittedVersion) < 2 {
		return nil, errors.New("invalid version")
	}

	versionStr := fmt.Sprintf("%s.%s", splittedVersion[0], splittedVersion[1])
	return &versionStr, nil
}
