package common

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"

	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/models"
)

type versionList []string

func (v versionList) Len() int {
	return len(v)
}

func (v versionList) Swap(i, j int) {
	v[i], v[j] = v[j], v[i]
}

func (v versionList) Less(i, j int) bool {
	v1, err := version.NewVersion(v[i])
	if err != nil {
		panic(err)
	}
	v2, err := version.NewVersion(v[j])
	if err != nil {
		panic(err)
	}
	return v1.LessThan(v2)
}

type VersionFinder struct {
}

func NewVersionFinder() *VersionFinder {
	v := new(VersionFinder)
	return v
}

func (v *VersionFinder) GetHighestVersion(versions []string) (string, error) {
	if len(versions) == 0 {
		return "", errors.New("there are no versions from which to pick a highest version")
	}
	sort.Sort(sort.Reverse(versionList(versions)))
	return versions[0], nil
}

func (v *VersionFinder) GetHigestOpenshiftVersionFromDefaultOsImages() (string, error) {
	defaultOsImagesFile, err := os.Open("../data/default_os_images.json")
	if err != nil {
		return "", err
	}
	osImagesFileBytes, err := io.ReadAll(defaultOsImagesFile)
	if err != nil {
		return "", err
	}
	var osImages models.OsImages
	err = json.Unmarshal(osImagesFileBytes, &osImages)
	if err != nil {
		return "", err
	}
	versions := []string{}
	for _, osImage := range osImages {
		versions = append(versions, *osImage.OpenshiftVersion)
	}
	return v.GetHighestVersion(versions)
}
