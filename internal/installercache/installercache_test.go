package installercache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/sirupsen/logrus"
)

var _ = Describe("installer cache", func() {
	var (
		ctrl        *gomock.Controller
		mockRelease *oc.MockRelease
		manager     *Installers
		cacheDir    string
		metricsAPI  *metrics.MockAPI
	)

	BeforeEach(func() {
		DeleteGracePeriod = 1 * time.Millisecond

		ctrl = gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)
		metricsAPI = metrics.NewMockAPI(ctrl)

		var err error
		cacheDir, err = os.MkdirTemp("/tmp", "cacheDir")
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Mkdir(filepath.Join(cacheDir, "quay.io"), 0755)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(filepath.Join(cacheDir, "quay.io"), "release-dev"), 0755)).To(Succeed())
		manager = New(cacheDir, 12, metricsAPI, logrus.New())
	})

	AfterEach(func() {
		os.RemoveAll(cacheDir)
	})

	testMetricsSentOnRelease := func(r *Release) {
		ctx := context.TODO()
		metricsAPI.EXPECT().InstallerReleaseCache(ctx, r.clusterID, r.releaseID, r.startTime, r.cached, r.extractDuration).Times(1)
		r.Release(ctx)
	}

	testGet := func(releaseID, version string, clusterId strfmt.UUID, expectCached bool) (string, string) {
		workdir := filepath.Join(cacheDir, "quay.io", "release-dev")
		fname := filepath.Join(workdir, releaseID)

		mockRelease.EXPECT().GetReleaseBinaryPath(
			gomock.Any(), gomock.Any(), version).
			Return(workdir, releaseID, fname, nil)
		mockRelease.EXPECT().Extract(gomock.Any(), releaseID,
			gomock.Any(), cacheDir, gomock.Any(), version).
			DoAndReturn(func(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string, version string) (string, error) {
				err := os.WriteFile(fname, []byte("abcde"), 0600)
				return "", err
			})
		l, err := manager.Get(releaseID, "mirror", "pull-secret", mockRelease, version, clusterId)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(l.releaseID).To(Equal(releaseID))
		Expect(l.clusterID).To(Equal(clusterId))
		Expect(l.startTime).ShouldNot(BeZero())
		Expect(l.cached).To(Equal(expectCached))
		Expect(l.metricsApi).To(Equal(metricsAPI))
		if !expectCached {
			Expect(l.extractDuration).ShouldNot(BeZero())
		}
		Expect(l.Path).ShouldNot(BeEmpty())

		time.Sleep(1 * time.Second)
		Expect(l.startTime.Before(time.Now())).To(BeTrue())
		testMetricsSentOnRelease(l)
		return fname, l.Path
	}
	It("evicts the oldest file", func() {
		clusterId := strfmt.UUID(uuid.New().String())
		r1, l1 := testGet("4.8", "4.8.0", clusterId, false)
		r2, l2 := testGet("4.9", "4.9.0", clusterId, false)
		r3, l3 := testGet("4.10", "4.10.0", clusterId, false)

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
		clusterId := strfmt.UUID(uuid.New().String())
		_, _ = testGet("4.8", "4.8.0", clusterId, false)
		r2, _ := testGet("4.9", "4.9.0", clusterId, false)
		r1, _ := testGet("4.8", "4.8.0", clusterId, true)
		r3, _ := testGet("4.10", "4.10.0", clusterId, false)

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
		clusterId := strfmt.UUID(uuid.New().String())
		r1, _ := testGet("4.8", "4.8.0", clusterId, false)
		r2, _ := testGet("4.9", "4.9.0", clusterId, false)
		r3, _ := testGet("4.10", "4.10.0", clusterId, false)

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
		clusterId := strfmt.UUID(uuid.New().String())
		version := "4.10.0"
		workdir := filepath.Join(cacheDir, "quay.io", "release-dev")
		fname := filepath.Join(workdir, releaseID)

		mockRelease.EXPECT().GetReleaseBinaryPath(
			releaseID, gomock.Any(), version).
			Return(workdir, releaseID, fname, nil)
		mockRelease.EXPECT().Extract(gomock.Any(), releaseID,
			gomock.Any(), cacheDir, gomock.Any(), version).
			DoAndReturn(func(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string, version string) (string, error) {
				err := os.WriteFile(fname, []byte("abcde"), 0600)
				return "", err
			})
		l, err := manager.Get(releaseID, releaseMirrorID, "pull-secret", mockRelease, version, clusterId)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(l.releaseID).To(Equal(releaseID))
		Expect(l.clusterID).To(Equal(clusterId))
		Expect(l.startTime).ShouldNot(BeZero())
		Expect(l.cached).To(BeFalse())
		Expect(l.extractDuration).ShouldNot(BeZero())
		Expect(l.Path).ShouldNot(BeEmpty())
		testMetricsSentOnRelease(l)
	})

	It("extracts without a mirror", func() {
		releaseID := "4.10-orig"
		releaseMirrorID := ""
		version := "4.10.0"
		clusterId := strfmt.UUID(uuid.NewString())
		workdir := filepath.Join(cacheDir, "quay.io", "release-dev")
		fname := filepath.Join(workdir, releaseID)

		mockRelease.EXPECT().GetReleaseBinaryPath(
			releaseID, gomock.Any(), version).
			Return(workdir, releaseID, fname, nil)
		mockRelease.EXPECT().Extract(gomock.Any(), releaseID,
			gomock.Any(), cacheDir, gomock.Any(), version).
			DoAndReturn(func(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string, version string) (string, error) {
				err := os.WriteFile(fname, []byte("abcde"), 0600)
				return "", err
			})
		l, err := manager.Get(releaseID, releaseMirrorID, "pull-secret", mockRelease, version, clusterId)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(l.releaseID).To(Equal(releaseID))
		Expect(l.clusterID).To(Equal(clusterId))
		Expect(l.startTime).ShouldNot(BeZero())
		Expect(l.cached).To(BeFalse())
		Expect(l.extractDuration).ShouldNot(BeZero())
		Expect(l.Path).ShouldNot(BeEmpty())
		testMetricsSentOnRelease(l)
	})

	It("Should have correct metrics on release object if error getting release URL", func() {
		releaseID := "4.10-orig"
		releaseMirrorID := ""
		version := "4.10.0"
		clusterId := strfmt.UUID(uuid.NewString())
		workdir := filepath.Join(cacheDir, "quay.io", "release-dev")
		fname := filepath.Join(workdir, releaseID)

		mockRelease.EXPECT().GetReleaseBinaryPath(
			releaseID, gomock.Any(), version).
			Return(workdir, releaseID, fname, errors.New("Some error!"))
		l, err := manager.Get(releaseID, releaseMirrorID, "pull-secret", mockRelease, version, clusterId)
		Expect(err).Should(HaveOccurred())
		Expect(l.releaseID).To(Equal(releaseID))
		Expect(l.clusterID).To(Equal(clusterId))
		Expect(l.startTime).ShouldNot(BeZero())
		Expect(l.cached).To(BeFalse())
		Expect(l.extractDuration).Should(BeZero())
		Expect(l.Path).Should(BeEmpty())
		testMetricsSentOnRelease(l)
	})

	It("Should have correct metrics on release object if error extracting release", func() {
		releaseID := "4.10-orig"
		releaseMirrorID := ""
		version := "4.10.0"
		clusterId := strfmt.UUID(uuid.NewString())
		workdir := filepath.Join(cacheDir, "quay.io", "release-dev")
		fname := filepath.Join(workdir, releaseID)

		mockRelease.EXPECT().GetReleaseBinaryPath(
			releaseID, gomock.Any(), version).
			Return(workdir, releaseID, fname, nil)
		mockRelease.EXPECT().Extract(gomock.Any(), releaseID,
			gomock.Any(), cacheDir, gomock.Any(), version).
			DoAndReturn(func(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string, version string) (string, error) {
				return "", errors.New("Some error!")
			})
		l, err := manager.Get(releaseID, releaseMirrorID, "pull-secret", mockRelease, version, clusterId)
		Expect(err).Should(HaveOccurred())
		Expect(l.releaseID).To(Equal(releaseID))
		Expect(l.clusterID).To(Equal(clusterId))
		Expect(l.startTime).ShouldNot(BeZero())
		Expect(l.cached).To(BeFalse())
		Expect(l.extractDuration).Should(BeZero())
		Expect(l.Path).Should(BeEmpty())
		testMetricsSentOnRelease(l)
	})
})

func TestInstallerCache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "installercache tests")
}
