package installercache

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/units"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var (
	linkPruningGracePeriod time.Duration = 5 * time.Minute
)

// Installers implements a thread safe LRU cache for ocp install binaries
// on the pod's ephermal file system. The number of binaries stored is
// limited by the storageCapacity parameter.
type Installers struct {
	sync.Mutex
	log logrus.FieldLogger
	// parent directory of the binary cache
	eventsHandler   eventsapi.Handler
	diskStatsHelper metrics.DiskStatsHelper
	config          Config
	metricsAPI      metrics.API
}

type Size int64

type Config struct {
	CacheDir string
	// MaxCapacity is the capacity of the installer cache and will be marshalled to a count of bytes from a form like "20 GiB"
	MaxCapacity Size `envconfig:"INSTALLER_CACHE_CAPACITY" default:"0"`
	// MaxReleaseSize is the expected maximum size of a single release and will be marshalled to a count of bytes from a form like "20 GiB"
	MaxReleaseSize Size `envconfig:"INSTALLER_CACHE_MAX_RELEASE_SIZE" default:"2GiB"`
	// ReleaseFetchRetryIntervalMicroseconds is the number of microseconds that the cache should wait before retrying the fetch of a release if unable to do so for capacity reasons.
	ReleaseFetchRetryInterval time.Duration `envconfig:"INSTALLER_CACHE_RELEASE_FETCH_RETRY_INTERVAL" default:"30s"`
}

func (s *Size) Decode(value string) error {
	bytes, err := units.ParseBase2Bytes(value)
	if err != nil {
		return err
	}
	*s = Size(bytes)
	return nil
}

type errorInsufficientCacheCapacity struct {
	Message string
}

func (e *errorInsufficientCacheCapacity) Error() string {
	return e.Message
}

type fileInfo struct {
	path string
	info os.FileInfo
}

func (fi *fileInfo) Compare(other *fileInfo) bool {
	//oldest file will be first in queue
	// Using micoseconds to make sure that the comparison is granular enough
	return fi.info.ModTime().UnixMicro() < other.info.ModTime().UnixMicro()
}

const (
	metricEventInstallerCacheRelease = "installercache.release.metrics"
)

type Release struct {
	Path          string
	eventsHandler eventsapi.Handler
	// startTime is the time at which the request was made to fetch the release.
	startTime time.Time
	// clusterID is the UUID of the cluster for which the release is being fetched.
	clusterID strfmt.UUID
	// releaseId is the release that is being fetched, for example "4.10.67-x86_64".
	releaseID string
	// cached is `true` if the release was found in the cache, otherwise `false`.
	cached bool
	// extractDuration is the amount of time taken to perform extraction, zero if no extraction took place.
	extractDuration float64
}

// Cleanup is called to signal that the caller has finished using the release and that resources may be released.
func (rl *Release) Cleanup(ctx context.Context) error {
	rl.eventsHandler.V2AddMetricsEvent(
		ctx, &rl.clusterID, nil, nil, "", models.EventSeverityInfo,
		metricEventInstallerCacheRelease,
		time.Now(),
		"release_id", rl.releaseID,
		"start_time", rl.startTime.Format(time.RFC3339),
		"end_time", time.Now().Format(time.RFC3339),
		"cached", rl.cached,
		"extract_duration", rl.extractDuration,
	)
	if err := os.Remove(rl.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// New constructs an installer cache with a given storage capacity
func New(config Config, eventsHandler eventsapi.Handler, metricsAPI metrics.API, diskStatsHelper metrics.DiskStatsHelper, log logrus.FieldLogger) (*Installers, error) {
	if config.MaxCapacity > 0 && config.MaxReleaseSize == 0 {
		return nil, fmt.Errorf("config.MaxReleaseSize (%d bytes) must not be zero", config.MaxReleaseSize)
	}
	if config.MaxCapacity > 0 && config.MaxReleaseSize > config.MaxCapacity {
		return nil, fmt.Errorf("config.MaxReleaseSize (%d bytes) must not be greater than config.MaxCapacity (%d bytes)", config.MaxReleaseSize, config.MaxCapacity)
	}
	return &Installers{
		log:             log,
		eventsHandler:   eventsHandler,
		diskStatsHelper: diskStatsHelper,
		config:          config,
		metricsAPI:      metricsAPI,
	}, nil
}

// Get returns the path to an openshift-baremetal-install binary extracted from
// the referenced release image. Tries the mirror release image first if it's set. It is safe for concurrent use. A cache of
// binaries is maintained to reduce re-downloading of the same release.
func (i *Installers) Get(ctx context.Context, releaseID, releaseIDMirror, pullSecret string, ocRelease oc.Release, ocpVersion string, clusterID strfmt.UUID) (*Release, error) {
	majorMinorVersion, err := ocRelease.GetMajorMinorVersion(i.log, releaseID, releaseIDMirror, pullSecret)
	if err != nil {
		i.log.Warnf("unable to get majorMinorVersion to record metric for %s falling back to full URI", releaseID)
		majorMinorVersion = "unknown"
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			release, err := i.get(releaseID, releaseIDMirror, pullSecret, ocRelease, ocpVersion, clusterID)
			if err == nil {
				i.metricsAPI.InstallerCacheGetReleaseCached(majorMinorVersion, release.cached)
				return release, nil
			}
			_, isCapacityError := err.(*errorInsufficientCacheCapacity)
			if !isCapacityError {
				return nil, errors.Wrapf(err, "failed to get installer path for release %s", releaseID)
			}
			time.Sleep(i.config.ReleaseFetchRetryInterval)
		}
	}
}

func (i *Installers) getDiskUsageIncludingHardlinks() (uint64, error) {
	usedBytes, _, err := i.diskStatsHelper.GetDiskUsage(i.config.CacheDir)
	if err != nil && !os.IsNotExist(err) {
		return 0, errors.Wrapf(err, "could not determine disk usage information for cache dir %s", i.config.CacheDir)
	}
	return usedBytes, nil
}

func (i *Installers) extractReleaseIfNeeded(path, releaseID, releaseIDMirror, pullSecret, ocpVersion string, ocRelease oc.Release) (extractDuration float64, cached bool, err error) {
	_, err = os.Stat(path)
	if err == nil {
		return 0, true, nil // release was found in the cache
	}
	if !os.IsNotExist(err) {
		return 0, false, err
	}
	usedBytes, err := i.getDiskUsageIncludingHardlinks()
	if err != nil && !os.IsNotExist(err) {
		return 0, false, errors.Wrapf(err, "could not determine disk usage information for cache dir %s", i.config.CacheDir)
	}
	if i.shouldEvict(int64(usedBytes)) && !i.evict() {
		return 0, false, &errorInsufficientCacheCapacity{Message: fmt.Sprintf("insufficient capacity in %s to store release", i.config.CacheDir)}
	}
	extractStartTime := time.Now()
	_, err = ocRelease.Extract(i.log, releaseID, releaseIDMirror, i.config.CacheDir, pullSecret, ocpVersion)
	if err != nil {
		return 0, false, err
	}
	return time.Since(extractStartTime).Seconds(), false, nil
}

func (i *Installers) get(releaseID, releaseIDMirror, pullSecret string, ocRelease oc.Release, ocpVersion string, clusterID strfmt.UUID) (*Release, error) {
	i.Lock()
	defer i.Unlock()

	release := &Release{
		eventsHandler: i.eventsHandler,
		clusterID:     clusterID,
		releaseID:     releaseID,
		startTime:     time.Now(),
	}

	workdir, binary, path, err := ocRelease.GetReleaseBinaryPath(releaseID, i.config.CacheDir, ocpVersion)
	if err != nil {
		return nil, err
	}
	release.extractDuration, release.cached, err = i.extractReleaseIfNeeded(path, releaseID, releaseIDMirror, pullSecret, ocpVersion, ocRelease)
	if err != nil {
		return nil, err
	}

	// update the file mtime to signal it was recently used
	err = os.Chtimes(path, time.Now(), time.Now())
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("failed to update release binary %s", path))
	}

	// return a new hard link to the binary file
	// the caller should delete the hard link when
	// it finishes working with the file
	release.Path, err = i.getLinkForReleasePath(workdir, path, binary)
	if err != nil {
		return nil, err
	}
	return release, nil
}

func (i *Installers) getLinkForReleasePath(workdir string, path string, binary string) (string, error) {
	for {
		link := filepath.Join(workdir, fmt.Sprintf("ln_%s_%s", uuid.NewString(), binary))
		err := os.Link(path, link)
		if err == nil {
			return link, nil
		}
		if !os.IsExist(err) {
			return "", errors.Wrap(err, fmt.Sprintf("failed to create hard link to binary %s", path))
		}
	}
}

func (i *Installers) shouldEvict(totalUsed int64) (shouldEvict bool) {
	if i.config.MaxCapacity == 0 {
		// The cache eviction is completely disabled.
		return false
	}
	return int64(i.config.MaxCapacity)-totalUsed < int64(i.config.MaxReleaseSize)
}

// Walk through the cacheDir and list the files recursively.
// If the total volume of the files reaches the capacity, delete
// the oldest ones.
//
// Locking must be done outside evict() to avoid contentions.
func (i *Installers) evict() bool {
	// store the file paths
	files := NewPriorityQueue(&fileInfo{})
	links := make([]*fileInfo, 0)
	var totalSize int64

	// visit process the file/dir pointed by path and store relevant
	// paths in a priority queue
	visit := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}
		//find hard links
		if strings.HasPrefix(info.Name(), "ln_") {
			links = append(links, &fileInfo{path, info})
			return nil
		}

		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink == 1 {
			if !ok {
				i.log.Warnf("could not determine hardlink status for %s - item will not be filtered", path)
			}
			files.Add(&fileInfo{path, info})
			totalSize += info.Size()
		}

		return nil
	}

	err := filepath.Walk(i.config.CacheDir, visit)
	if err != nil {
		if !os.IsNotExist(err) { //ignore first invocation where the cacheDir does not exist
			i.log.WithError(err).Errorf("release binary eviction failed to inspect directory %s", i.config.CacheDir)
		}
		return false
	}

	// TODO: We might want to consider if we need to do this longer term, in theory every hardlink should be automatically freed.
	i.pruneExpiredHardLinks(links, linkPruningGracePeriod)

	// delete the oldest file if necessary
	evicted := false
	for i.shouldEvict(totalSize) && files.Len() > 0 {
		finfo, _ := files.Pop()
		totalSize -= finfo.info.Size()
		//remove the file
		if err := i.evictFile(finfo.path); err != nil {
			i.log.WithError(err).Errorf("failed to evict file %s", finfo.path)
			continue
		}
		evicted = true
	}
	i.metricsAPI.InstallerCacheReleaseEvicted(evicted)
	return evicted
}

func (i *Installers) evictFile(filePath string) error {
	i.log.Infof("evicting binary file %s due to storage pressure", filePath)
	err := os.Remove(filePath)
	if err != nil {
		return err
	}
	// if the parent directory was left empty,
	// remove it to avoid dangling directories
	parentDir := path.Dir(filePath)
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return os.Remove(parentDir)
	}
	return nil
}

// pruneExpiredHardLinks removes any hardlinks that have been around for too long
// the grace period is used to determine which links should be removed.
func (i *Installers) pruneExpiredHardLinks(links []*fileInfo, gracePeriod time.Duration) {
	for idx := 0; idx < len(links); idx++ {
		finfo := links[idx]
		graceTime := time.Now().Add(-1 * gracePeriod)
		grace := graceTime.Unix()
		if finfo.info.ModTime().Unix() < grace {
			i.log.Infof("attempting to prune hard link %s", finfo.path)
			if err := os.Remove(finfo.path); err != nil {
				i.log.WithError(err).Errorf("failed to prune hard link %s", finfo.path)
			}
		}
	}
}
