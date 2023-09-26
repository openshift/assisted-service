package imageservice

import (
	"fmt"
	"net/url"
	"path"

	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

type BootArtifactURLs struct {
	KernelURL string
	RootFSURL string
	InitrdURL string
}

const BootArtifactsPath = "/boot-artifacts"

type ShortImageURLPrefix string

const ByAPIKeyPath ShortImageURLPrefix = "byapikey"
const ByIDPath ShortImageURLPrefix = "byid"
const ByTokenPath ShortImageURLPrefix = "bytoken"

const MinimalISOFilename string = "minimal.iso"
const FullISOFilename string = "full.iso"

func KernelURL(baseURL, version, arch string, insecure bool) (string, error) {
	return buildURL(baseURL, fmt.Sprintf("%s/kernel", BootArtifactsPath), insecure, map[string]string{
		"version": version,
		"arch":    arch,
	})
}

func RootFSURL(baseURL, version, arch string, insecure bool) (string, error) {
	return buildURL(baseURL, fmt.Sprintf("%s/rootfs", BootArtifactsPath), insecure, map[string]string{
		"version": version,
		"arch":    arch,
	})
}

func InitrdURL(baseURL, imageID, version, arch string, insecure bool) (string, error) {
	path := fmt.Sprintf("/images/%s/pxe-initrd", imageID)
	return buildURL(baseURL, path, insecure, map[string]string{
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

func ShortImageURL(baseURL string, prefix ShortImageURLPrefix, token, version, arch, isoType string) (string, error) {
	var isoFilename string

	switch isoType {
	case string(models.ImageTypeFullIso):
		isoFilename = FullISOFilename
	case string(models.ImageTypeMinimalIso):
		isoFilename = MinimalISOFilename
	default:
		return "", fmt.Errorf("failed generating image url: %s iso type does not exist", isoType)
	}

	path := fmt.Sprintf("%s/%s/%s/%s/%s", prefix, token, version, arch, isoFilename)
	return buildURL(baseURL, path, false, map[string]string{})
}

func GetBootArtifactURLs(baseURL, imageID string, osImage *models.OsImage, insecure bool) (*BootArtifactURLs, error) {
	version := *osImage.OpenshiftVersion
	arch := *osImage.CPUArchitecture
	kernelUrl, err := KernelURL(baseURL, version, arch, insecure)
	if err != nil {
		return nil, fmt.Errorf("failed generating kernel url: %w", err)
	}
	rootfsUrl, err := RootFSURL(baseURL, version, arch, insecure)
	if err != nil {
		return nil, fmt.Errorf("failed generating rootfs url: %w", err)
	}
	initrdUrl, err := InitrdURL(baseURL, imageID, version, arch, insecure)
	if err != nil {
		return nil, fmt.Errorf("failed generating initrd url: %w", err)
	}
	return &BootArtifactURLs{
		KernelURL: kernelUrl,
		RootFSURL: rootfsUrl,
		InitrdURL: initrdUrl,
	}, nil
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

// ParseDownloadURL parse an image service download URL and returns the image type, the arch and the version.
// This function can parse /images/ URLs and shorter /by.../ URLs.
func ParseDownloadURL(downloadURL string) (string, string, string, error) {
	var imageType, version, arch string

	u, err := url.Parse(downloadURL)
	if err != nil {
		return "", "", "", errors.Wrap(err, "failed to parse download URL")
	}

	// The URL contains a query string
	// then it's a long image URL
	vals := u.Query()
	if len(vals) > 0 {
		imageType = vals.Get("type")
		version = vals.Get("version")
		arch = vals.Get("arch")
		return imageType, arch, version, nil
	}

	// shorter URL
	rest, isoFilename := path.Split(u.Path)
	rest, arch = path.Split(path.Clean(rest))
	_, version = path.Split(path.Clean(rest))

	switch isoFilename {
	case FullISOFilename:
		imageType = string(models.ImageTypeFullIso)
	case MinimalISOFilename:
		imageType = string(models.ImageTypeMinimalIso)
	}

	return imageType, arch, version, nil
}
