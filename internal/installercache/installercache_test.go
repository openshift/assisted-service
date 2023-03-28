package installercache

import (
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("installer cache", func() {
	var (
		cache      *ReleaseCache
		r1, r2, r3 string
	)

	BeforeEach(func() {
		cache = New(2, logrus.New())
	})

	AfterEach(func() {
		_ = os.Remove(r1)
		_ = os.Remove(r2)
		_ = os.Remove(r3)
	})

	addFile := func(releaseID string) *release {
		cache.releases.ContainsOrAdd(releaseID, &release{})
		r, _ := cache.releases.Get(releaseID)

		r.Lock()
		defer r.Unlock()
		f, err := ioutil.TempFile("/tmp", releaseID)
		Expect(err).ShouldNot(HaveOccurred())
		r.Path = f.Name()
		return r
	}

	It("evicts the oldest file", func() {
		//mimic the operation of Get
		r1 = addFile("4.8").Path
		r2 = addFile("4.9").Path
		r3 = addFile("4.10").Path
		//verify that the oldest file was deleted
		_, err := os.Stat(r1)
		Expect(os.IsNotExist(err)).To(BeTrue())
		_, err = os.Stat(r2)
		Expect(os.IsNotExist(err)).To(BeFalse())
		_, err = os.Stat(r3)
		Expect(os.IsNotExist(err)).To(BeFalse())
	})

	It("same releases are stored once", func() {
		//mimic the operation of Get
		r1 = addFile("4.8").Path
		r2 = addFile("4.8").Path
		r3 = addFile("4.10").Path
		//verify that the oldest file was deleted
		_, err := os.Stat(r1)
		Expect(os.IsNotExist(err)).To(BeFalse())
		_, err = os.Stat(r3)
		Expect(os.IsNotExist(err)).To(BeFalse())
	})
})

func TestInstallerCache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "installercache tests")
}
