package installercache

import (
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/unix"
)

var _ = Describe("GetDiskUsageForDirectory", func() {

	var (
		ctrl             *gomock.Controller
		unixHelper       *MockUnixHelper
		fileSystemHelper FileSystemHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		unixHelper = NewMockUnixHelper(ctrl)
		fileSystemHelper = NewOSFileSystemnHelper(unixHelper)
	})

	It("should calculate usage from block information", func() {
		unixHelper.EXPECT().
			Statfs("/foo/bar", gomock.Any()).
			DoAndReturn(func(path string, stat *unix.Statfs_t) error {
				stat.Blocks = 1000
				stat.Bfree = 500
				stat.Bsize = 1024
				return nil
			}).
			Times(1)

		usedBytes, usagePercent, err := fileSystemHelper.GetDiskUsageForDirectory("/foo/bar")
		Expect(err).ToNot(HaveOccurred())
		Expect(usedBytes).To(BeEquivalentTo(512000))
		Expect(usagePercent).To(BeEquivalentTo(50))
	})

	It("should handle zero block count gracefully and avoid division by zero", func() {
		unixHelper.EXPECT().
			Statfs("/foo/bar", gomock.Any()).
			DoAndReturn(func(path string, stat *unix.Statfs_t) error {
				stat.Blocks = 0
				stat.Bfree = 500
				stat.Bsize = 1024
				return nil
			}).
			Times(1)

		usedBytes, usagePercent, err := fileSystemHelper.GetDiskUsageForDirectory("/foo/bar")
		Expect(err).To(HaveOccurred())
		Expect(usedBytes).To(BeEquivalentTo(0))
		Expect(usagePercent).To(BeEquivalentTo(0))
	})

})
