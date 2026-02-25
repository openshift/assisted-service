package versions

import (
	context "context"
	"encoding/json"
	"fmt"
	"sync"

	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	models "github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type KeyFormat int

const (
	KeyFormatFull KeyFormat = iota
	KeyFormatMajorMinorCPUArchitecture
	KeyFormatMajorMinor
)

var (
	errNotFound = errors.New("must gather version not found")
)

type MustGatherVersionCache struct {
	versions map[string]MustGatherVersion
	lock     *sync.RWMutex
}

func NewMustGatherVersionCache() MustGatherVersionCache {
	return MustGatherVersionCache{
		versions: make(map[string]MustGatherVersion),
		lock:     &sync.RWMutex{},
	}
}

func NewMustGatherVersionCacheFromJSON(txt string) (MustGatherVersionCache, error) {
	ret := NewMustGatherVersionCache()

	err := json.Unmarshal([]byte(txt), &ret.versions)
	if err != nil {
		return ret, fmt.Errorf("failed to unmarshal must gather versions: %w", err)
	}

	return ret, nil
}

func NewMustGatherVersionCacheFromMustGatherImages(mustGatherImages []aiv1beta1.MustGatherImage) (MustGatherVersionCache, error) {
	ret := NewMustGatherVersionCache()

	for _, mustGatherImage := range mustGatherImages {
		key, err := getKeyFromMustGatherImage(mustGatherImage)
		if err != nil {
			return ret, fmt.Errorf("failed to get key from must gather image: %w", err)
		}

		if ret.versions[key] == nil {
			ret.versions[key] = make(MustGatherVersion)
		}

		ret.versions[key][mustGatherImage.Name] = mustGatherImage.Url
	}

	return ret, nil
}

func getMustGatherImages(
	log logrus.FieldLogger,
	openshiftVersion,
	cpuArchitecture,
	pullSecret,
	releaseImageMirror string,
	mustGatherVersionCache MustGatherVersionCache,
	getReleaseImage func(ctx context.Context, openshiftVersion, cpuArchitecture, pullSecret string) (*models.ReleaseImage, error),
	releaseHandler oc.Release,
) (MustGatherVersion, error) {
	// Check if ocp must-gather image is already in the cache
	ret, err := mustGatherVersionCache.GetMustGatherVersion(openshiftVersion, cpuArchitecture)
	if err != nil && !errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("failed to get must gather version: %w", err)
	}

	// If the cache had a hit
	if ret != nil {
		// If the ocp image is in the cache, return the cache
		if _, ok := ret["ocp"]; ok {
			return ret, nil
		}
	}

	// If not, fetch it from the release image and add it to the cache
	releaseImage, err := getReleaseImage(context.Background(), openshiftVersion, cpuArchitecture, pullSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get release image: %w", err)
	}
	ocpMustGatherImage, err := releaseHandler.GetMustGatherImage(log, *releaseImage.URL, releaseImageMirror, pullSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get must gather image: %w", err)
	}

	ret = MustGatherVersion{"ocp": ocpMustGatherImage}

	err = mustGatherVersionCache.AddMustGatherVersion(openshiftVersion, cpuArchitecture, ret)
	if err != nil {
		return nil, fmt.Errorf("failed to add must gather version: %w", err)
	}

	// Return the merged result from cache (includes pre-populated entries like cnv, odf, lso)
	ret, err = mustGatherVersionCache.GetMustGatherVersion(openshiftVersion, cpuArchitecture)
	if err != nil {
		return nil, fmt.Errorf("failed to get must gather version after add: %w", err)
	}

	return ret, nil
}

func getKeyFromMustGatherImage(mustGatherImage aiv1beta1.MustGatherImage) (string, error) {
	if mustGatherImage.CPUArchitecture == "" {
		return getKey(mustGatherImage.OpenshiftVersion, "", KeyFormatMajorMinor)
	}

	return getKey(mustGatherImage.OpenshiftVersion, mustGatherImage.CPUArchitecture, KeyFormatFull)
}

func getKey(openshiftVersion, cpuArchitecture string, format KeyFormat) (string, error) {
	var majorMinor string

	// Validations
	if (format == KeyFormatFull || format == KeyFormatMajorMinorCPUArchitecture) && cpuArchitecture == "" {
		return "", errors.New("cpu architecture is required")
	}

	if format == KeyFormatMajorMinorCPUArchitecture || format == KeyFormatMajorMinor {
		majMinorVersion, err := common.GetMajorMinorVersion(openshiftVersion)
		if err != nil {
			return "", fmt.Errorf("failed to get major minor version: %w", err)
		}

		if majMinorVersion == nil {
			return "", errors.New("GetMajorMinorVersion returned an empty value")
		}

		majorMinor = *majMinorVersion
	}

	// Create output
	switch format {
	case KeyFormatFull:
		return fmt.Sprintf("%s-%s", openshiftVersion, cpuArchitecture), nil
	case KeyFormatMajorMinorCPUArchitecture:
		return fmt.Sprintf("%s-%s", majorMinor, cpuArchitecture), nil
	case KeyFormatMajorMinor:
		return majorMinor, nil
	default:
		return "", fmt.Errorf("invalid key format: %d", format)
	}
}

func (c MustGatherVersionCache) GetMustGatherVersion(openshiftVersion, cpuArchitecture string) (MustGatherVersion, error) {
	ret, err := c.getMustGatherVersion(openshiftVersion, cpuArchitecture, KeyFormatFull)
	if err != nil && !errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("failed to get must gather version for KeyFullFormat: %w", err)
	}

	if ret != nil {
		return ret, nil
	}

	ret, err = c.getMustGatherVersion(openshiftVersion, cpuArchitecture, KeyFormatMajorMinorCPUArchitecture)
	if err != nil && !errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("failed to get must gather version for KeyFormatMajorMinorCPUArchitecture: %w", err)
	}

	if ret != nil {
		return ret, nil
	}

	ret, err = c.getMustGatherVersion(openshiftVersion, cpuArchitecture, KeyFormatMajorMinor)
	if err != nil && !errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("failed to get must gather version for KeyFormatMajorMinor: %w", err)
	}

	if ret != nil {
		return ret, nil
	}

	return nil, errNotFound
}

func (c MustGatherVersionCache) AddMustGatherVersion(openshiftVersion, cpuArchitecture string, mustGatherVersion MustGatherVersion) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	key, err := getKey(openshiftVersion, cpuArchitecture, KeyFormatFull)
	if err != nil {
		return fmt.Errorf("failed to compute full key: %w", err)
	}

	_, ok := c.versions[key]
	if !ok {
		c.versions[key] = make(MustGatherVersion)
	}

	for name, url := range mustGatherVersion {
		c.versions[key][name] = url
	}

	return nil
}

func (c MustGatherVersionCache) ToJSON() (string, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	json, err := json.Marshal(c.versions)
	if err != nil {
		return "", fmt.Errorf("failed to marshal must gather versions: %w", err)
	}

	return string(json), nil
}

func (c MustGatherVersionCache) Size() int {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return len(c.versions)
}

func (c MustGatherVersionCache) getMustGatherVersion(openshiftVersion, cpuArchitecture string, format KeyFormat) (MustGatherVersion, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if (format == KeyFormatFull || format == KeyFormatMajorMinorCPUArchitecture) && cpuArchitecture == "" {
		return nil, errNotFound
	}

	key, err := getKey(openshiftVersion, cpuArchitecture, format)
	if err != nil {
		return nil, fmt.Errorf("failed to get full key: %w", err)
	}

	ret, ok := c.versions[key]
	if ok {
		return ret, nil
	}

	return ret, errNotFound
}
