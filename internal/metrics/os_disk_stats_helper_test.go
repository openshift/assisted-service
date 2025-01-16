package metrics

import (
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("OS disk stats helper", func() {

	var (
		tempDir         string
		log             *logrus.Logger
		diskStatsHelper *OSDiskStatsHelper
	)

	BeforeEach(func() {
		log = logrus.New()
		log.SetOutput(io.Discard)
		diskStatsHelper = NewOSDiskStatsHelper(log)
		var err error
		tempDir, err = os.MkdirTemp("", "disk_stats_helper_test")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
	})

	var writeDummyFile = func(sizeInBytes int64) string {
		file, err := os.CreateTemp(tempDir, "dummy-file")
		Expect(err).ToNot(HaveOccurred())
		defer file.Close()

		err = file.Truncate(sizeInBytes)
		Expect(err).ToNot(HaveOccurred())
		return file.Name()
	}

	It("should retrieve correct stats for an empty directory", func() {
		usageBytes, freeBytes, err := diskStatsHelper.GetDiskUsage(tempDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(usageBytes).To(BeEquivalentTo(0))
		Expect(freeBytes).To(BeNumerically(">", 0))
	})

	It("should correctly retrieve stats for a directory with some space used", func() {
		writeDummyFile(16384)
		writeDummyFile(16384)
		usageBytes, freeBytes, err := diskStatsHelper.GetDiskUsage(tempDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(usageBytes).To(BeEquivalentTo(32768))
		Expect(freeBytes).To(BeNumerically(">", 0))
	})

	It("hardlinks should be correctly handled", func() {
		writeDummyFile(16384)

		// Create a hardlink to a second file
		path := writeDummyFile(16384)
		link := filepath.Join(tempDir, "ln_"+uuid.NewString())
		err := os.Link(path, link)
		Expect(err).ToNot((HaveOccurred()))

		usageBytes, _, err := diskStatsHelper.GetDiskUsage(tempDir)
		Expect(err).ToNot(HaveOccurred())

		// We should only be counting the size of the two actual files.
		Expect(usageBytes).To(BeEquivalentTo(32768))

		// Delete the main file link
		err = os.Remove(path)
		Expect(err).ToNot(HaveOccurred())

		// Grab usage again
		usageBytes, _, err = diskStatsHelper.GetDiskUsage(tempDir)
		Expect(err).ToNot(HaveOccurred())

		// We expect the same result as the hardlink is still pointing to the old file
		Expect(usageBytes).To(BeEquivalentTo(32768))

		// Now get rid of the link
		err = os.Remove(link)
		Expect(err).ToNot(HaveOccurred())

		// Grab usage again
		usageBytes, _, err = diskStatsHelper.GetDiskUsage(tempDir)
		Expect(err).ToNot(HaveOccurred())

		// We expect to use less space as all references to the file inodes have been removed.
		Expect(usageBytes).To(BeEquivalentTo(16384))

	})

})
