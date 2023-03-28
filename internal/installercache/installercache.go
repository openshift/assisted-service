package installercache

import (
	"os"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/sirupsen/logrus"
)

//TODO: TwoQueueCache which support lfu policy does
//      not support onEvict hook that we need in order
//      to clear the files. Replace the cache when they
//      do.
type ReleaseCache struct {
	releases *lru.Cache[string, *release]
	log      logrus.FieldLogger
}

type release struct {
	sync.Mutex
	Path string
}

func New(capacity int, log logrus.FieldLogger) *ReleaseCache {
	rc := ReleaseCache{
		log: log,
	}
	c, err := lru.NewWithEvict(capacity, rc.onEvicted)
	if err != nil {
		log.WithError(err).Fatalln("Failed to initialize the release binaries cache")
	}
	rc.releases = c
	return &rc
}

func (i ReleaseCache) onEvicted(key string, r *release) {
	if r == nil {
		i.log.Errorf("Evicting entry %s with unexpected nil value", key)
		return
	}

	r.Lock()
	defer r.Unlock()

	i.log.Infof("Evicting entry %s %v", key, r.Path)
	err := os.Remove(r.Path)
	if err != nil {
		i.log.Errorf("Failed to clear %s", r.Path)
	}
}

// Get returns the path to an openshift-baremetal-install binary extracted from
// the referenced release image. Tries the mirror release image first if it's set.
// It is safe for concurrent use. A cache of binaries is maintained to reduce
// re-downloading of the same release.
func (i *ReleaseCache) Get(releaseID, releaseIDMirror, cacheDir, pullSecret string, platformType models.PlatformType, icspFile string) (*release, error) {
	var r *release
	var ok bool
	if r, ok = i.releases.Get(releaseID); !ok {
		r = &release{}
	}

	r.Lock()
	//if the entry was evicted before the lock took hold
	//or the entry was not in the cache in the first place
	//add it to the cache and mark the path as empty
	//so the extraction will take place
	//the lock will make sure that the binary will not
	//be deleted until after it is invoked.
	if !i.releases.Contains(releaseID) {
		r.Path = ""
		i.releases.Add(releaseID, r)
	}

	var path string
	var err error
	if r.Path == "" { //cache miss
		mirrorRegistriesBuilder := mirrorregistries.New()
		path, err = oc.NewRelease(&executer.CommonExecuter{}, oc.Config{
			MaxTries: oc.DefaultTries, RetryDelay: oc.DefaltRetryDelay}, mirrorRegistriesBuilder).Extract(i.log, releaseID, releaseIDMirror, cacheDir, pullSecret, platformType, icspFile)
		if err != nil {
			r.Unlock()
			return &release{}, err
		}
		r.Path = path
	}
	return r, nil
}
