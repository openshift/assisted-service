package installercache

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

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
// the referenced release image. It is safe for concurrent use. A cache of
// binaries is maintained to reduce re-downloading of the same release.
func Get(releaseID, cacheDir, pullSecret string, log logrus.FieldLogger) (string, error) {
	r := cache.Get(releaseID)
	r.Lock()
	defer r.Unlock()

	// cache miss
	if r.path == "" {
		// write pull secret to a temp file
		ps, err := ioutil.TempFile("", "registry-config")
		if err != nil {
			return "", err
		}
		defer func() {
			ps.Close()
			os.Remove(ps.Name())
		}()
		_, err = ps.Write([]byte(pullSecret))
		if err != nil {
			return "", err
		}
		// flush the buffer to ensure the file can be read
		ps.Close()

		workdir := filepath.Join(cacheDir, releaseID)
		log.Infof("extracting openshift-baremetal-install binary to %s", workdir)
		err = os.MkdirAll(workdir, 0755)
		if err != nil {
			return "", err
		}

		// extract openshift-baremetal-install binary from the release
		cmd := exec.Command("oc", "adm", "release", "extract", "--command=openshift-baremetal-install",
			"--registry-config="+ps.Name(), "--to="+workdir, releaseID) // #nosec G204
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err = cmd.Run()
		if err != nil {
			log.Error("error running \"oc adm release extract\" for release %s", releaseID)
			log.Error(out.String())
			return "", err
		}

		// set path
		r.path = filepath.Join(workdir, "openshift-baremetal-install")
	}
	return r.path, nil
}
