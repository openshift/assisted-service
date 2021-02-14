package s3wrapper

import (
	"context"
	"io/ioutil"
	"os"
	"strings"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetFile", func() {
	var (
		ctrl     *gomock.Controller
		mockAPI  *MockAPI
		cacheDir string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockAPI = NewMockAPI(ctrl)
		var err error
		cacheDir, err = ioutil.TempDir("", "file_cache_test")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		os.RemoveAll(cacheDir)
		ClearFileCache()
	})

	It("Downloads files only when not present in the cache", func() {
		ctx := context.Background()

		objName1 := "my-test-object"
		content1 := "hello world"
		r1 := ioutil.NopCloser(strings.NewReader(content1))
		mockAPI.EXPECT().Download(ctx, objName1).Times(1).Return(r1, int64(len(content1)), nil)

		objName2 := "my-other-object"
		content2 := "HELLO WORLD"
		r2 := ioutil.NopCloser(strings.NewReader(content2))
		mockAPI.EXPECT().Download(ctx, objName2).Times(1).Return(r2, int64(len(content2)), nil)

		path1, err := GetFile(ctx, mockAPI, objName1, cacheDir, false)
		Expect(err).ToNot(HaveOccurred())
		validateFileContent(path1, content1)

		path2, err := GetFile(ctx, mockAPI, objName2, cacheDir, false)
		Expect(err).ToNot(HaveOccurred())
		validateFileContent(path2, content2)

		// get both files again to ensure download isn't called more than once
		path1, err = GetFile(ctx, mockAPI, objName1, cacheDir, false)
		Expect(err).ToNot(HaveOccurred())
		validateFileContent(path1, content1)

		path2, err = GetFile(ctx, mockAPI, objName2, cacheDir, false)
		Expect(err).ToNot(HaveOccurred())
		validateFileContent(path2, content2)
	})

	It("Keeps separate cache entries for public vs private", func() {
		ctx := context.Background()

		objName := "my-test-object"
		content := "hello world"
		r := ioutil.NopCloser(strings.NewReader(content))
		mockAPI.EXPECT().Download(ctx, objName).Times(1).Return(r, int64(len(content)), nil)

		contentPub := "HELLO WORLD"
		rPub := ioutil.NopCloser(strings.NewReader(contentPub))
		mockAPI.EXPECT().DownloadPublic(ctx, objName).Times(1).Return(rPub, int64(len(contentPub)), nil)

		path, err := GetFile(ctx, mockAPI, objName, cacheDir, false)
		Expect(err).ToNot(HaveOccurred())
		validateFileContent(path, content)

		pathPub, err := GetFile(ctx, mockAPI, objName, cacheDir, true)
		Expect(err).ToNot(HaveOccurred())
		validateFileContent(pathPub, contentPub)
	})
})

func validateFileContent(path string, content string) {
	fileContent, err := ioutil.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())
	Expect(string(fileContent)).To(Equal(content))
}
