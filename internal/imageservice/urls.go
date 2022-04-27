package imageservice

import (
	"fmt"
	"net/url"
	"path"

	"github.com/pkg/errors"
)

const BootArtifactsPath = "/boot-artifacts"

func KernelURL(baseURL, version, arch string) (string, error) {
	return buildURL(baseURL, fmt.Sprintf("%s/kernel", BootArtifactsPath), true, map[string]string{
		"version": version,
		"arch":    arch,
	})
}

func RootFSURL(baseURL, version, arch string) (string, error) {
	return buildURL(baseURL, fmt.Sprintf("%s/rootfs", BootArtifactsPath), true, map[string]string{
		"version": version,
		"arch":    arch,
	})
}

func InitrdURL(baseURL, imageID, version, arch string) (string, error) {
	path := fmt.Sprintf("/images/%s/pxe-initrd", imageID)
	return buildURL(baseURL, path, true, map[string]string{
		"version": version,
		"arch":    arch,
	})
}

func ImageURL(baseURL, imageID, version, arch, isoType string) (string, error) {
	path := fmt.Sprintf("/images/%s", imageID)
	return buildURL(baseURL, path, false, map[string]string{
		"type":    isoType,
		"version": version,
		"arch":    arch,
	})
}

func buildURL(baseURL string, suffix string, insecure bool, params map[string]string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse image service base URL")
	}
	downloadURL := url.URL{
		Scheme: base.Scheme,
		Host:   base.Host,
		Path:   path.Join(base.Path, suffix),
	}
	queryValues := url.Values{}
	for k, v := range params {
		if v != "" {
			queryValues.Set(k, v)
		}
	}
	downloadURL.RawQuery = queryValues.Encode()
	if insecure {
		downloadURL.Scheme = "http"
	}
	return downloadURL.String(), nil
}
