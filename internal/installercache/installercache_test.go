package installercache

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/sirupsen/logrus"
)

var _ = Describe("installer cache", func() {
	var (
		ctrl        *gomock.Controller
		mockRelease *oc.MockRelease
		manager     *Installers
		cacheDir    string
	)

	BeforeEach(func() {
		DeleteGracePeriod = 1 * time.Millisecond

		ctrl = gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)

		var err error
		cacheDir, err = ioutil.TempDir("/tmp", "cacheDir")
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Mkdir(filepath.Join(cacheDir, "quay.io"), 0755)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(filepath.Join(cacheDir, "quay.io"), "release-dev"), 0755)).To(Succeed())
		manager = New(cacheDir, 12, logrus.New())
	})

	AfterEach(func() {
		os.RemoveAll(cacheDir)
	})

	testGet := func(releaseID string) (string, string) {
		workdir := filepath.Join(cacheDir, "quay.io", "release-dev")
		fname := filepath.Join(workdir, releaseID)

		mockRelease.EXPECT().GetReleaseBinaryPath(
			gomock.Any(), gomock.Any()).
			Return(workdir, releaseID, fname)
		mockRelease.EXPECT().Extract(gomock.Any(), releaseID,
			gomock.Any(), cacheDir, gomock.Any()).
			DoAndReturn(func(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string) (string, error) {
				err := os.WriteFile(fname, []byte("abcde"), 0600)
				return "", err
			})
		l, err := manager.Get(releaseID, "mirror", "pull-secret", mockRelease)
		Expect(err).ShouldNot(HaveOccurred())

		time.Sleep(1 * time.Second)
		return fname, l.Path
	}
	It("evicts the oldest file", func() {
		r1, l1 := testGet("4.8")
		r2, l2 := testGet("4.9")
		r3, l3 := testGet("4.10")

		By("verify that the oldest file was deleted")
		_, err := os.Stat(r1)
		Expect(os.IsNotExist(err)).To(BeTrue())
		_, err = os.Stat(r2)
		Expect(os.IsNotExist(err)).To(BeFalse())
		_, err = os.Stat(r3)
		Expect(os.IsNotExist(err)).To(BeFalse())

		By("verify that the links were purged")
		manager.evict()
		_, err = os.Stat(l1)
		Expect(os.IsNotExist(err)).To(BeTrue())
		_, err = os.Stat(l2)
		Expect(os.IsNotExist(err)).To(BeTrue())
		_, err = os.Stat(l3)
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	It("exising files access time is updated", func() {
		_, _ = testGet("4.8")
		r2, _ := testGet("4.9")
		r1, _ := testGet("4.8")
		r3, _ := testGet("4.10")

		By("verify that the oldest file was deleted")
		_, err := os.Stat(r1)
		Expect(os.IsNotExist(err)).To(BeFalse())
		_, err = os.Stat(r2)
		Expect(os.IsNotExist(err)).To(BeTrue())
		_, err = os.Stat(r3)
		Expect(os.IsNotExist(err)).To(BeFalse())
	})

	It("when cache limit is not set eviction is skipped", func() {
		manager.storageCapacity = 0

		r1, _ := testGet("4.8")
		r2, _ := testGet("4.9")
		r3, _ := testGet("4.10")

		By("verify that the no file was deleted")
		_, err := os.Stat(r1)
		Expect(os.IsNotExist(err)).To(BeFalse())
		_, err = os.Stat(r2)
		Expect(os.IsNotExist(err)).To(BeFalse())
		_, err = os.Stat(r3)
		Expect(os.IsNotExist(err)).To(BeFalse())

	})

	It("extracts from the mirror", func() {
		releaseID := "4.10-orig"
		releaseMirrorID := "4.10-mirror"
		workdir := filepath.Join(cacheDir, "quay.io", "release-dev")
		fname := filepath.Join(workdir, releaseID)

		mockRelease.EXPECT().GetReleaseBinaryPath(
			releaseMirrorID, gomock.Any()).
			Return(workdir, releaseID, fname)
		mockRelease.EXPECT().Extract(gomock.Any(), releaseID,
			gomock.Any(), cacheDir, gomock.Any()).
			DoAndReturn(func(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string) (string, error) {
				err := os.WriteFile(fname, []byte("abcde"), 0600)
				return "", err
			})
		_, err := manager.Get(releaseID, releaseMirrorID, "pull-secret", mockRelease)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("extracts without a mirror", func() {
		releaseID := "4.10-orig"
		releaseMirrorID := ""
		workdir := filepath.Join(cacheDir, "quay.io", "release-dev")
		fname := filepath.Join(workdir, releaseID)

		mockRelease.EXPECT().GetReleaseBinaryPath(
			releaseID, gomock.Any()).
			Return(workdir, releaseID, fname)
		mockRelease.EXPECT().Extract(gomock.Any(), releaseID,
			gomock.Any(), cacheDir, gomock.Any()).
			DoAndReturn(func(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string) (string, error) {
				err := os.WriteFile(fname, []byte("abcde"), 0600)
				return "", err
			})
		_, err := manager.Get(releaseID, releaseMirrorID, "pull-secret", mockRelease)
		Expect(err).ShouldNot(HaveOccurred())
	})
})

func TestInstallerCache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "installercache tests")
}
