package s3wrapper

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/sirupsen/logrus"
)

var _ = Describe("s3filesystem", func() {
	var (
		ctx            = context.Background()
		log            = logrus.New()
		deleteTime     time.Duration
		client         *FSClient
		ctrl           *gomock.Controller
		mockMetricsAPI *metrics.MockAPI
		now            time.Time
		baseDir        string
		dataStr        = "hello world"
		objKey         = "discovery-image-d183c403-d27b-42e1-b0a4-1274ea1a5d77.iso"
		objKey2        = "discovery-image-f318a87b-ba57-4c7e-ae5f-ee562a6d1e8c.iso"
	)
	BeforeEach(func() {
		log.SetOutput(io.Discard)
		var err error
		baseDir, err = os.MkdirTemp("", "test")
		Expect(err).Should(BeNil())

		ctrl = gomock.NewController(GinkgoT())
		mockMetricsAPI = metrics.NewMockAPI(ctrl)
		client = &FSClient{basedir: baseDir, log: log}
		deleteTime, _ = time.ParseDuration("60m")
		now, _ = time.Parse(time.RFC3339, "2020-01-01T10:00:00+00:00")
	})
	AfterEach(func() {
		err := os.RemoveAll(baseDir)
		Expect(err).Should(BeNil())
	})
	It("upload_download", func() {
		mockMetricsAPI.EXPECT().FileSystemUsage(gomock.Any()).Times(1)
		expLen := len(dataStr)
		err := client.Upload(ctx, []byte(dataStr), objKey)
		Expect(err).Should(BeNil())

		size, err := client.GetObjectSizeBytes(ctx, objKey)
		Expect(err).Should(BeNil())
		Expect(size).To(Equal(int64(expLen)))

		buf := make([]byte, expLen+5) // Buf with some extra room
		reader, downloadLength, err := client.Download(ctx, objKey)
		Expect(err).Should(BeNil())
		length, err := reader.Read(buf)
		Expect(err).Should(BeNil())
		Expect(length).To(Equal(expLen))
		Expect(downloadLength).To(Equal(int64(expLen)))
	})
	It("uploadfile_download", func() {
		mockMetricsAPI.EXPECT().FileSystemUsage(gomock.Any()).Times(1)
		expLen := len(dataStr)
		filePath, _ := createFileObject(client.basedir, objKey, now)
		err := client.UploadFile(ctx, filePath, objKey)
		Expect(err).Should(BeNil())

		size, err := client.GetObjectSizeBytes(ctx, objKey)
		Expect(err).Should(BeNil())
		Expect(size).To(Equal(int64(expLen)))

		buf := make([]byte, expLen+5) // Buf with some extra room
		reader, downloadLength, err := client.Download(ctx, objKey)
		Expect(err).Should(BeNil())
		length, err := reader.Read(buf)
		Expect(err).Should(BeNil())
		Expect(length).To(Equal(expLen))
		Expect(downloadLength).To(Equal(int64(expLen)))
	})
	It("uploadstream_download", func() {
		mockMetricsAPI.EXPECT().FileSystemUsage(gomock.Any()).Times(1)
		expLen := len(dataStr)
		filePath, _ := createFileObject(client.basedir, "foo", now)
		fileReader, err := os.Open(filePath)
		Expect(err).Should(BeNil())
		err = client.UploadStream(ctx, fileReader, objKey)
		Expect(err).Should(BeNil())
		fileReader.Close()

		size, err := client.GetObjectSizeBytes(ctx, objKey)
		Expect(err).Should(BeNil())
		Expect(size).To(Equal(int64(expLen)))

		buf := make([]byte, expLen+5) // Buf with some extra room
		reader, downloadLength, err := client.Download(ctx, objKey)
		Expect(err).Should(BeNil())
		length, err := reader.Read(buf)
		Expect(err).Should(BeNil())
		Expect(length).To(Equal(expLen))
		Expect(downloadLength).To(Equal(int64(expLen)))
	})
	It("doesobjectexist_delete", func() {
		mockMetricsAPI.EXPECT().FileSystemUsage(gomock.Any()).Times(2)
		err := client.Upload(ctx, []byte(dataStr), objKey)
		Expect(err).Should(BeNil())

		exists, err := client.DoesObjectExist(ctx, objKey)
		Expect(err).Should(BeNil())
		Expect(exists).To(Equal(true))

		existed, err := client.DeleteObject(ctx, objKey)
		Expect(existed).To(Equal(true))
		Expect(err).Should(BeNil())

		exists, err = client.DoesObjectExist(ctx, objKey)
		Expect(err).Should(BeNil())
		Expect(exists).To(Equal(false))
	})
	It("expiration", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T09:30:00+00:00") // Long ago
		createFileObject(client.basedir, objKey, imgCreatedAt)
		createFileObject(client.basedir, objKey2, imgCreatedAt)

		called := 0
		mockMetricsAPI.EXPECT().FileSystemUsage(gomock.Any()).Times(2)
		client.ExpireObjects(ctx, "discovery-image-", deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = called + 1 })
		Expect(called).To(Equal(2))

		exists, err := client.DoesObjectExist(ctx, objKey)
		Expect(err).Should(BeNil())
		Expect(exists).To(Equal(false))

		exists, err = client.DoesObjectExist(ctx, objKey2)
		Expect(err).Should(BeNil())
		Expect(exists).To(Equal(false))
	})
	It("expire_not_expired_image", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T09:30:00+00:00") // 30 minutes ago
		filePath, info := createFileObject(client.basedir, objKey, imgCreatedAt)
		called := false
		mockMetricsAPI.EXPECT().FileSystemUsage(gomock.Any()).Times(1)
		client.handleFile(ctx, log, filePath, info, now, deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = true })
		Expect(called).To(Equal(false))
	})
	It("expire_expired_image", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T08:00:00+00:00") // Two hours ago
		filePath, info := createFileObject(client.basedir, objKey, imgCreatedAt)
		called := false
		mockMetricsAPI.EXPECT().FileSystemUsage(gomock.Any()).Times(1)
		client.handleFile(ctx, log, filePath, info, now, deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = true })
		Expect(called).To(Equal(true))
	})
	It("expire_delete_error", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T08:00:00+00:00") // Two hours ago
		filePath, info := createFileObject(client.basedir, objKey, imgCreatedAt)
		os.Remove(filePath)
		called := false
		client.handleFile(ctx, log, filePath, info, now, deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = true })
		Expect(called).To(Equal(false))
	})

	It("ListObjectByPrefix lists the correct object without a leading slash", func() {
		_, _ = createFileObject(client.basedir, "dir/other/file", now)
		_, _ = createFileObject(client.basedir, "dir/other/file2", now)
		_, _ = createFileObject(client.basedir, "dir/file", now)
		_, _ = createFileObject(client.basedir, "dir2/file", now)

		var objects []string
		var err error
		containsObj := func(obj string) bool {
			for _, o := range objects {
				if obj == o {
					return true
				}
			}
			return false
		}

		objects, err = client.ListObjectsByPrefix(ctx, "dir/other")
		Expect(err).NotTo(HaveOccurred())
		Expect(len(objects)).To(Equal(2))
		Expect(containsObj("dir/other/file")).To(BeTrue(), "file list %v does not contain \"dir/other/file\"", objects)
		Expect(containsObj("dir/other/file2")).To(BeTrue(), "file list %v does not contain \"dir/other/file2\"", objects)

		objects, err = client.ListObjectsByPrefix(ctx, "dir2")
		Expect(err).NotTo(HaveOccurred())
		Expect(len(objects)).To(Equal(1))
		Expect(objects[0]).To(Equal("dir2/file"))

		objects, err = client.ListObjectsByPrefix(ctx, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(len(objects)).To(Equal(4))
		Expect(containsObj("dir/other/file")).To(BeTrue(), "file list %v does not contain \"dir/other/file\"", objects)
		Expect(containsObj("dir/other/file2")).To(BeTrue(), "file list %v does not contain \"dir/other/file2\"", objects)
		Expect(containsObj("dir/file")).To(BeTrue(), "file list %v does not contain \"dir/file\"", objects)
		Expect(containsObj("dir2/file")).To(BeTrue(), "file list %v does not contain \"dir2/file\"", objects)
	})

	AfterEach(func() {
		os.RemoveAll(baseDir)
	})
})

func createFileObject(baseDir, objKey string, imgCreatedAt time.Time) (string, os.FileInfo) {
	filePath := filepath.Join(baseDir, objKey)
	Expect(os.MkdirAll(filepath.Dir(filePath), 0755)).To(Succeed())
	err := os.WriteFile(filePath, []byte("Hello world"), 0600)
	Expect(err).Should(BeNil())
	err = os.Chtimes(filePath, imgCreatedAt, imgCreatedAt)
	Expect(err).Should(BeNil())
	info, err := os.Stat(filePath)
	Expect(err).Should(BeNil())
	return filePath, info
}
