package installercache

import (
	"sync"

	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/sirupsen/logrus"
)

type installers struct {
	sync.Mutex
	releases map[string]*release
}

type release struct {
	sync.Mutex
	path string
}

var cache installers = installers{
	releases: make(map[string]*release),
}

// Get returns a release resource for the given release ID
func (i *installers) Get(releaseID string) *release {
	i.Lock()
	defer i.Unlock()

	r, present := i.releases[releaseID]
	if !present {
		r = &release{}
		i.releases[releaseID] = r
	}
	return r
}

// Get returns the path to an openshift-baremetal-install binary extracted from
// the referenced release image. Tries the mirror release image first if it's set. It is safe for concurrent use. A cache of
// binaries is maintained to reduce re-downloading of the same release.
func Get(releaseID, releaseIDMirror, cacheDir, pullSecret string, platformType models.PlatformType, icspFile string, log logrus.FieldLogger) (string, error) {
	r := cache.Get(releaseID)
	r.Lock()
	defer r.Unlock()

	var path string
	var err error
	//cache miss
	if r.path == "" {
		mirrorRegistriesBuilder := mirrorregistries.New()
		path, err = oc.NewRelease(&executer.CommonExecuter{}, oc.Config{
			MaxTries: oc.DefaultTries, RetryDelay: oc.DefaltRetryDelay}, mirrorRegistriesBuilder).Extract(log, releaseID, releaseIDMirror, cacheDir, pullSecret, platformType, icspFile)
		if err != nil {
			return "", err
		}
		r.path = path
	}
	return r.path, nil
}
