package installercache

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("release event", func() {

	var (
		ctx           context.Context
		ctrl          *gomock.Controller
		eventsHandler *eventsapi.MockHandler
	)

	BeforeEach(func() {
		ctx = context.TODO()
		ctrl = gomock.NewController(GinkgoT())
		eventsHandler = eventsapi.NewMockHandler(ctrl)
	})

	It("should send correct fields when release event is sent", func() {
		startTime, err := time.Parse(time.RFC3339, "2025-01-07T16:51:10Z")
		Expect(err).ToNot(HaveOccurred())
		clusterID := strfmt.UUID(uuid.NewString())
		releaseID := "quay.io/openshift-release-dev/ocp-release:4.16.28-x86_64"
		r := &Release{
			eventsHandler:   eventsHandler,
			startTime:       startTime,
			clusterID:       clusterID,
			releaseID:       releaseID,
			cached:          false,
			extractDuration: 19.5,
		}
		eventsHandler.EXPECT().V2AddMetricsEvent(
			ctx, &clusterID, nil, nil, "", models.EventSeverityInfo, metricEventInstallerCacheRelease, gomock.Any(),
			"release_id", r.releaseID,
			"start_time", "2025-01-07T16:51:10Z",
			"end_time", gomock.Any(),
			"cached", r.cached,
			"extract_duration", r.extractDuration,
		).Times(1)
		Expect(r.Cleanup(ctx)).To(Succeed())
	})
})

var _ = Describe("installer cache", func() {
	var (
		ctrl            *gomock.Controller
		mockRelease     *oc.MockRelease
		manager         *Installers
		cacheDir        string
		eventsHandler   *eventsapi.MockHandler
		metricsAPI      *metrics.MockAPI
		ctx             context.Context
		diskStatsHelper metrics.DiskStatsHelper
	)

	var getInstallerCacheConfig = func(maxCapacity int64, maxReleaseSize int64) Config {
		return Config{
			CacheDir:                  cacheDir,
			MaxCapacity:               Size(maxCapacity),
			MaxReleaseSize:            Size(maxReleaseSize),
			ReleaseFetchRetryInterval: 1 * time.Microsecond,
		}
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		diskStatsHelper = metrics.NewOSDiskStatsHelper(logrus.New())
		mockRelease = oc.NewMockRelease(ctrl)
		eventsHandler = eventsapi.NewMockHandler(ctrl)
		metricsAPI = metrics.NewMockAPI(ctrl)
		var err error
		cacheDir, err = os.MkdirTemp("/tmp", "cacheDir")
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Mkdir(filepath.Join(cacheDir, "quay.io"), 0755)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(filepath.Join(cacheDir, "quay.io"), "release-dev"), 0755)).To(Succeed())
		manager, err = New(getInstallerCacheConfig(12, 5), eventsHandler, metricsAPI, diskStatsHelper, logrus.New())
		Expect(err).NotTo(HaveOccurred())
		ctx = context.TODO()
	})

	AfterEach(func() {
		os.RemoveAll(cacheDir)
	})

	expectEventsSent := func() {
		eventsHandler.EXPECT().V2AddMetricsEvent(
			ctx, gomock.Any(), nil, nil, "", models.EventSeverityInfo, metricEventInstallerCacheRelease, gomock.Any(),
			gomock.Any(),
		).AnyTimes()
	}

	mockReleaseCalls := func(releaseID string, version string) {
		workdir := filepath.Join(manager.config.CacheDir, "quay.io", "release-dev")
		fname := filepath.Join(workdir, releaseID)

		writeMockedReleaseToDisk := func(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string, version string) (string, error) {
			dir, _ := filepath.Split(fname)
			err := os.MkdirAll(dir, 0700)
			Expect(err).ToNot(HaveOccurred())
			time.Sleep(10 * time.Millisecond) // Add a small amount of latency to simulate extraction
			err = os.WriteFile(fname, []byte("abcde"), 0600)
			return "", err
		}

		mockRelease.EXPECT().GetReleaseBinaryPath(
			gomock.Any(), gomock.Any(), version).
			Return(workdir, releaseID, fname, nil).AnyTimes()

		mockRelease.EXPECT().Extract(gomock.Any(), releaseID,
			gomock.Any(), manager.config.CacheDir, gomock.Any(), version).
			DoAndReturn(writeMockedReleaseToDisk).AnyTimes()

		metricsAPI.EXPECT().InstallerCacheReleaseEvicted(gomock.Any()).AnyTimes()

	}

	testGet := func(releaseID, version string, clusterID strfmt.UUID, expectCached bool, expectedMajorMinorVersion string) (string, string) {
		workdir := filepath.Join(manager.config.CacheDir, "quay.io", "release-dev")
		fname := filepath.Join(workdir, releaseID)
		if !expectCached {
			mockReleaseCalls(releaseID, version)
		}
		expectEventsSent()
		mockReleaseCalls(releaseID, version)
		expectEventsSent()
		mockRelease.EXPECT().GetMajorMinorVersion(gomock.Any(), releaseID, gomock.Any(), gomock.Any()).Return(expectedMajorMinorVersion, nil).Times(1)
		metricsAPI.EXPECT().InstallerCacheGetReleaseCached(expectedMajorMinorVersion, gomock.Any()).AnyTimes()
		l, err := manager.Get(ctx, releaseID, "mirror", "pull-secret", mockRelease, version, clusterID)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(l.releaseID).To(Equal(releaseID))
		Expect(l.clusterID).To(Equal(clusterID))
		Expect(l.startTime).ShouldNot(BeZero())
		Expect(l.cached).To(Equal(expectCached))
		Expect(l.eventsHandler).To(Equal(eventsHandler))
		if !expectCached {
			Expect(l.extractDuration).ShouldNot(BeZero())
		}
		Expect(l.Path).ShouldNot(BeEmpty())
		Expect(l.startTime.Before(time.Now())).To(BeTrue())
		Expect(l.Cleanup(context.TODO())).To(Succeed())
		return fname, l.Path
	}

	type test struct {
		releaseID         string
		version           string
		clusterID         strfmt.UUID
		majorMinorVersion string
	}

	getUsedBytesForDirectory := func(directory string) uint64 {
		var totalBytes uint64
		seenInodes := make(map[uint64]bool)
		err := filepath.Walk(directory, func(path string, fileInfo os.FileInfo, err error) error {
			if err != nil {
				if _, ok := err.(*os.PathError); ok {
					// Something deleted the file before we could walk it
					// count this as zero bytes
					return nil
				}
				return err
			}
			stat, ok := fileInfo.Sys().(*syscall.Stat_t)
			Expect(ok).To(BeTrue())
			if !fileInfo.IsDir() && !seenInodes[stat.Ino] {
				totalBytes += uint64(fileInfo.Size())
				seenInodes[stat.Ino] = true
			}
			return nil
		})
		Expect(err).ToNot(HaveOccurred())
		return totalBytes
	}

	runTest := func(t test, manager *Installers) (*Release, error) {
		expectEventsSent()
		mockReleaseCalls(t.releaseID, t.version)
		mockRelease.EXPECT().GetMajorMinorVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(t.majorMinorVersion, nil).AnyTimes()
		metricsAPI.EXPECT().InstallerCacheGetReleaseCached(t.majorMinorVersion, gomock.Any()).AnyTimes()
		return manager.Get(ctx, t.releaseID, "mirror", "pull-secret", mockRelease, t.version, t.clusterID)
	}

	cleanup := func(release *Release) {
		if release != nil {
			time.Sleep(2 * time.Millisecond)
			Expect(release.Cleanup(ctx)).To(Succeed())
		}
	}

	handleError := func(errMutex *sync.Mutex, err error, reportedError *error) {
		if err != nil {
			errMutex.Lock()
			if reportedError == nil {
				*reportedError = err
			}
			errMutex.Unlock()
			return
		}
	}

	getLogger := func() *logrus.Logger {
		log := logrus.New()
		log.SetOutput(io.Discard)
		return log
	}

	// runParallelTest launches installercache.Get tests in parallel
	// releases are automatically cleaned up as they are gathered
	// returns the first error encountered or nil if no error encountered.
	runParallelTest := func(maxCapacity int64, maxReleaseSize int64, tests []test) error {
		var err error
		manager, err = New(getInstallerCacheConfig(maxCapacity, maxReleaseSize), eventsHandler, metricsAPI, diskStatsHelper, getLogger())
		Expect(err).ToNot(HaveOccurred())
		var wg sync.WaitGroup
		var reportedError error
		var errMutex sync.Mutex
		for _, t := range tests {
			t.clusterID = strfmt.UUID(uuid.NewString())
			wg.Add(1)
			go func(t test) {
				defer wg.Done()
				release, err := runTest(t, manager)
				handleError(&errMutex, err, &reportedError)
				cleanup(release)
			}(t)
		}
		wg.Wait()
		return reportedError
	}

	It("should correctly handle multiple requests for the same release at the same time", func() {
		maxCapacity := int64(10)
		maxReleaseSize := int64(5)
		err := runParallelTest(maxCapacity, maxReleaseSize, []test{
			{releaseID: "4.17.11-x86_64", version: "4.17.11", majorMinorVersion: "4.17"},
			{releaseID: "4.17.11-x86_64", version: "4.17.11", majorMinorVersion: "4.17"},
			{releaseID: "4.17.11-x86_64", version: "4.17.11", majorMinorVersion: "4.17"},
			{releaseID: "4.17.11-x86_64", version: "4.17.11", majorMinorVersion: "4.17"},
			{releaseID: "4.17.11-x86_64", version: "4.17.11", majorMinorVersion: "4.17"},
		})
		Expect(err).ToNot(HaveOccurred())
		// Now measure disk usage, we should be under the cache size
		Expect(getUsedBytesForDirectory(manager.config.CacheDir)).To(BeNumerically("<=", maxCapacity))
	})

	It("should consistently handle multiple requests for different releases at the same time", func() {
		maxCapacity := int64(25)
		maxReleaseSize := int64(5)
		err := runParallelTest(maxCapacity, maxReleaseSize, []test{
			{releaseID: "4.17.11-x86_64", version: "4.17.11", majorMinorVersion: "4.17"},
			{releaseID: "4.18.11-x86_64", version: "4.18.11", majorMinorVersion: "4.17"},
			{releaseID: "4.19.11-x86_64", version: "4.19.11", majorMinorVersion: "4.17"},
			{releaseID: "4.20.11-x86_64", version: "4.20.11", majorMinorVersion: "4.17"},
			{releaseID: "4.21.11-x86_64", version: "4.21.11", majorMinorVersion: "4.17"},
		})
		Expect(err).ToNot(HaveOccurred())
		// Now measure disk usage, we should be under the cache size
		Expect(getUsedBytesForDirectory(manager.config.CacheDir)).To(BeNumerically("<=", maxCapacity))
	})

	It("should stay within the cache limit where there is only sufficient space for one release", func() {
		maxCapacity := int64(5)
		maxReleaseSize := int64(5)
		err := runParallelTest(maxCapacity, maxReleaseSize, []test{
			{releaseID: "4.17.11-x86_64", version: "4.17.11", majorMinorVersion: "4.17"},
			{releaseID: "4.18.11-x86_64", version: "4.18.11", majorMinorVersion: "4.18"},
		})
		Expect(err).ToNot(HaveOccurred())
		// Now measure disk usage, we should be under the cache size
		Expect(getUsedBytesForDirectory(manager.config.CacheDir)).To(BeNumerically("<=", maxCapacity))

		// Now assert that a retry would work, there should be enough space for another release
		// use a brand new release ID to prove we are not hitting cache here.
		err = runParallelTest(maxCapacity, maxReleaseSize, []test{
			{releaseID: "4.19.11-x86_64", version: "4.19.11", majorMinorVersion: "4.19"},
		})
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should raise error on construction if max release size is larger than cache and cache is enabled", func() {
		_, err := New(getInstallerCacheConfig(5, 10), eventsHandler, metricsAPI, diskStatsHelper, logrus.New())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("config.MaxReleaseSize (10 bytes) must not be greater than config.MaxCapacity (5 bytes)"))
	})

	It("Should raise error on construction if max release size is zero and cache is enabled", func() {
		_, err := New(getInstallerCacheConfig(5, 0), eventsHandler, metricsAPI, diskStatsHelper, logrus.New())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("config.MaxReleaseSize (0 bytes) must not be zero"))
	})

	It("Should not raise error on construction if max release size is larger than cache and cache eviction is disabled", func() {
		_, err := New(getInstallerCacheConfig(0, 10), eventsHandler, metricsAPI, diskStatsHelper, logrus.New())
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should not raise error on construction if max release size is zero and cache eviction is disabled", func() {
		_, err := New(getInstallerCacheConfig(0, 0), eventsHandler, metricsAPI, diskStatsHelper, logrus.New())
		Expect(err).ToNot(HaveOccurred())
	})

	It("when cache limit is zero - eviction is skipped", func() {
		var err error
		manager, err = New(getInstallerCacheConfig(0, 5), eventsHandler, metricsAPI, diskStatsHelper, logrus.New())
		Expect(err).ToNot(HaveOccurred())
		clusterId := strfmt.UUID(uuid.New().String())
		r1, _ := testGet("4.8", "4.8.0", clusterId, false, "4.8")
		r2, _ := testGet("4.9", "4.9.0", clusterId, false, "4.9")
		r3, _ := testGet("4.10", "4.10.0", clusterId, false, "4.10")

		By("verify that the no file was deleted")
		_, err = os.Stat(r1)
		Expect(os.IsNotExist(err)).To(BeFalse())
		_, err = os.Stat(r2)
		Expect(os.IsNotExist(err)).To(BeFalse())
		_, err = os.Stat(r3)
		Expect(os.IsNotExist(err)).To(BeFalse())
	})

	It("existing files access time is updated", func() {
		clusterId := strfmt.UUID(uuid.New().String())
		_, _ = testGet("4.8", "4.8.0", clusterId, false, "4.8")
		r2, _ := testGet("4.9", "4.9.0", clusterId, false, "4.9")
		r1, _ := testGet("4.8", "4.8.0", clusterId, true, "4.8")
		r3, _ := testGet("4.10", "4.10.0", clusterId, false, "4.10")

		By("verify that the oldest file was deleted")
		_, err := os.Stat(r1)
		Expect(os.IsNotExist(err)).To(BeFalse())
		_, err = os.Stat(r2)
		Expect(os.IsNotExist(err)).To(BeTrue())
		_, err = os.Stat(r3)
		Expect(os.IsNotExist(err)).To(BeFalse())
		// Now measure disk usage, we should be under the cache size
		Expect(getUsedBytesForDirectory(manager.config.CacheDir)).To(BeNumerically("<=", manager.config.MaxCapacity))
	})

	It("evicts the oldest file", func() {
		clusterId := strfmt.UUID(uuid.New().String())
		r1, _ := testGet("4.8", "4.8.0", clusterId, false, "4.8")
		r2, _ := testGet("4.9", "4.9.0", clusterId, false, "4.9")
		r3, _ := testGet("4.10", "4.10.0", clusterId, false, "4.10")

		By("verify that the oldest file was deleted")
		_, err := os.Stat(r1)
		Expect(os.IsNotExist(err)).To(BeTrue())
		_, err = os.Stat(r2)
		Expect(os.IsNotExist(err)).To(BeFalse())
		_, err = os.Stat(r3)
		Expect(os.IsNotExist(err)).To(BeFalse())

		// Now measure disk usage, we should be under the cache size
		Expect(getUsedBytesForDirectory(manager.config.CacheDir)).To(BeNumerically("<=", manager.config.MaxCapacity))
	})

	It("extracts a release", func() {
		releaseID := "4.10-orig"
		releaseMirrorID := ""
		version := "4.10.0"
		majorMinorVersion := "4.10"
		clusterID := strfmt.UUID(uuid.NewString())
		mockRelease.EXPECT().GetMajorMinorVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(majorMinorVersion, nil).AnyTimes()
		mockReleaseCalls(releaseID, version)
		metricsAPI.EXPECT().InstallerCacheGetReleaseCached(majorMinorVersion, false)
		l, err := manager.Get(ctx, releaseID, releaseMirrorID, "pull-secret", mockRelease, version, clusterID)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(l.releaseID).To(Equal(releaseID))
		Expect(l.clusterID).To(Equal(clusterID))
		Expect(l.startTime).ShouldNot(BeZero())
		Expect(l.cached).To(BeFalse())
		Expect(l.extractDuration).ShouldNot(BeZero())
		Expect(l.Path).ShouldNot(BeEmpty())
		expectEventsSent()
		Expect(l.Cleanup(ctx)).To(Succeed())
		// Now measure disk usage, we should be under the cache size
		Expect(getUsedBytesForDirectory(manager.config.CacheDir)).To(BeNumerically("<=", manager.config.MaxCapacity))
	})

	It("should remove expired links while leaving non expired links intact", func() {

		numberOfLinks := 10
		numberOfExpiredLinks := 5
		directory, err := os.MkdirTemp("", "testPruneExpiredHardLinks")
		Expect(err).ToNot(HaveOccurred())

		defer os.RemoveAll(directory)

		for i := 0; i < numberOfLinks; i++ {
			var someFile *os.File
			someFile, err = os.CreateTemp(directory, "somefile")
			Expect(err).ToNot(HaveOccurred())
			linkPath := filepath.Join(directory, fmt.Sprintf("ln_%s", uuid.NewString()))
			err = os.Link(someFile.Name(), linkPath)
			Expect(err).ToNot(HaveOccurred())
			if i > numberOfExpiredLinks-1 {
				err = os.Chtimes(linkPath, time.Now().Add(-10*time.Minute), time.Now().Add(-10*time.Minute))
				Expect(err).ToNot(HaveOccurred())
			}
		}

		links := make([]*fileInfo, 0)
		err = filepath.Walk(directory, func(path string, info fs.FileInfo, err error) error {
			if strings.HasPrefix(info.Name(), "ln_") {
				links = append(links, &fileInfo{path, info})
			}
			return nil
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(len(links)).To(Equal(10))

		manager.pruneExpiredHardLinks(links, linkPruningGracePeriod)

		linkCount := 0
		err = filepath.Walk(directory, func(path string, info fs.FileInfo, err error) error {
			if strings.HasPrefix(info.Name(), "ln_") {
				linkCount++
			}
			return nil
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(linkCount).To(Equal(numberOfLinks - numberOfExpiredLinks))
	})

})

func TestInstallerCache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "installercache tests")
}
