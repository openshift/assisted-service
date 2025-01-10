package installercache

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-openapi/strfmt"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var (
	DeleteGracePeriod   time.Duration = -5 * time.Minute
	CacheLimitThreshold               = 0.8
)

// Installers implements a thread safe LRU cache for ocp install binaries
// on the pod's ephermal file system. The number of binaries stored is
// limited by the storageCapacity parameter.
type Installers struct {
	sync.Mutex
	log logrus.FieldLogger
	// total capcity of the allowed storage (in bytes)
	storageCapacity int64
	// parent directory of the binary cache
	cacheDir      string
	eventsHandler eventsapi.Handler
}

type fileInfo struct {
	path string
	info os.FileInfo
}

func (fi *fileInfo) Compare(other *fileInfo) bool {
	//oldest file will be first in queue
	return fi.info.ModTime().Unix() < other.info.ModTime().Unix()
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

// Cleanup is called to signal that the caller has finished using the relase and that resources may be released.
func (rl *Release) Cleanup(ctx context.Context) {
	if err := os.Remove(rl.Path); err != nil {
		logrus.New().WithError(err).Errorf("Failed to delete release link %s", rl.Path)
	}
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
}

// New constructs an installer cache with a given storage capacity
func New(cacheDir string, storageCapacity int64, eventsHandler eventsapi.Handler, log logrus.FieldLogger) *Installers {
	return &Installers{
		log:             log,
		storageCapacity: storageCapacity,
		cacheDir:        cacheDir,
		eventsHandler:   eventsHandler,
	}
}

// Get returns the path to an openshift-baremetal-install binary extracted from
// the referenced release image. Tries the mirror release image first if it's set. It is safe for concurrent use. A cache of
// binaries is maintained to reduce re-downloading of the same release.
func (i *Installers) Get(ctx context.Context, releaseID, releaseIDMirror, pullSecret string, ocRelease oc.Release, ocpVersion string, clusterID strfmt.UUID) (*Release, error) {
	i.Lock()
	defer i.Unlock()

	release := &Release{
		eventsHandler: i.eventsHandler,
		clusterID:     clusterID,
		releaseID:     releaseID,
		startTime:     time.Now(),
	}

	var workdir, binary, path string
	var err error

	workdir, binary, path, err = ocRelease.GetReleaseBinaryPath(releaseID, i.cacheDir, ocpVersion)
	if err != nil {
		return nil, err
	}
	if _, err = os.Stat(path); os.IsNotExist(err) {
		extractStartTime := time.Now()
		//evict older files if necessary
		i.evict()

		//extract the binary
		_, err = ocRelease.Extract(i.log, releaseID, releaseIDMirror, i.cacheDir, pullSecret, ocpVersion)
		if err != nil {
			return nil, err
		}
		release.extractDuration = time.Since(extractStartTime).Seconds()
	} else {
		release.cached = true
		//update the file mtime to signal it was recently used
		err = os.Chtimes(path, time.Now(), time.Now())
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("Failed to update release binary %s", path))
		}
	}
	// return a new hard link to the binary file
	// the caller should delete the hard link when
	// it finishes working with the file
	link := filepath.Join(workdir, "ln_"+fmt.Sprint(time.Now().Unix())+
		"_"+binary)
	err = os.Link(path, link)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("Failed to create hard link to binary %s", path))
	}
	release.Path = link
	return release, nil
}

// Walk through the cacheDir and list the files recursively.
// If the total volume of the files reaches the capacity, delete
// the oldest ones.
//
// Locking must be done outside evict() to avoid contentions.
func (i *Installers) evict() {
	//if cache limit is undefined skip eviction
	if i.storageCapacity == 0 {
		return
	}

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

		//save the other files based on their mod time
		files.Add(&fileInfo{path, info})
		totalSize += info.Size()
		return nil
	}

	err := filepath.Walk(i.cacheDir, visit)
	if err != nil {
		if !os.IsNotExist(err) { //ignore first invocation where the cacheDir does not exist
			i.log.WithError(err).Errorf("release binary eviction failed to inspect directory %s", i.cacheDir)
		}
		return
	}

	//prune the hard links just in case the deletion of resources
	//in ignition.go did not succeeded as expected
	for idx := 0; idx < len(links); idx++ {
		finfo := links[idx]
		//Allow a grace period of 5 minutes from the link creation time
		//to ensure the link is not being used.
		grace := time.Now().Add(DeleteGracePeriod).Unix()
		if finfo.info.ModTime().Unix() < grace {
			os.Remove(finfo.path)
		}
	}

	//delete the oldest file if necessary
	for totalSize >= int64(float64(i.storageCapacity)*CacheLimitThreshold) {
		finfo, _ := files.Pop()
		totalSize -= finfo.info.Size()
		//remove the file
		if err := i.evictFile(finfo.path); err != nil {
			i.log.WithError(err).Errorf("failed to evict file %s", finfo.path)
		}
	}
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
