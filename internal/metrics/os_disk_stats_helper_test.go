package metrics

import (
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("OS disk stats helper", func() {

	var (
		tempDir         string
		diskStatsHelper *OSDiskStatsHelper = NewOSDiskStatsHelper()
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "disk_stats_helper_test")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
	})

	var writeDummyFile = func(sizeInBytes int64) {
		file, err := os.CreateTemp(tempDir, "dummy-file")
		Expect(err).ToNot(HaveOccurred())
		defer file.Close()

		err = file.Truncate(sizeInBytes)
		Expect(err).ToNot(HaveOccurred())
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

})
