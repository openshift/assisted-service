package common

import (
	"errors"
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

func BaseVersionGreaterOrEqual(version, versionMayGreaterThan string) (bool, error) {
	// return version >= versionMayGreaterThan
	version = strings.Split(version, "-")[0]
	versionMayGreaterThan = strings.Split(versionMayGreaterThan, "-")[0]

	return VersionGreaterOrEqual(versionMayGreaterThan, version)
}

func BaseVersionLessThan(version, versionMayLessThan string) (bool, error) {
	isGraterOrEqual, err := BaseVersionGreaterOrEqual(version, versionMayLessThan)
	if err != nil {
		return false, err
	}
	return !isGraterOrEqual, nil
}

// BaseVersionEqual Compare Major and Minor of 2 different versions
func BaseVersionEqual(version1, versionMayEqual string) (bool, error) {
	version1 = strings.Split(version1, "-")[0]
	versionMayEqual = strings.Split(versionMayEqual, "-")[0]

	v1 := strings.Split(version1, ".")
	v2 := strings.Split(versionMayEqual, ".")

	if len(v1) < 2 || len(v2) < 2 {
		return false, errors.New("invalid version")
	}

	return v1[0] == v2[0] && v1[1] == v2[1], nil
}
