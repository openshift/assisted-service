package s3wrapper

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
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
		filePath, _ := createFileObject(client.basedir, objKey, now, nil)
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
		filePath, _ := createFileObject(client.basedir, "foo", now, nil)
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
		createFileObject(client.basedir, objKey, imgCreatedAt, nil)
		createFileObject(client.basedir, objKey2, imgCreatedAt, nil)

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
		filePath, info := createFileObject(client.basedir, objKey, imgCreatedAt, nil)
		called := false
		mockMetricsAPI.EXPECT().FileSystemUsage(gomock.Any()).Times(1)
		client.handleFile(ctx, log, filePath, info, now, deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = true })
		Expect(called).To(Equal(false))
	})
	It("expire_expired_image", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T08:00:00+00:00") // Two hours ago
		filePath, info := createFileObject(client.basedir, objKey, imgCreatedAt, nil)
		called := false
		mockMetricsAPI.EXPECT().FileSystemUsage(gomock.Any()).Times(1)
		client.handleFile(ctx, log, filePath, info, now, deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = true })
		Expect(called).To(Equal(true))
	})
	It("expire_delete_error", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T08:00:00+00:00") // Two hours ago
		filePath, info := createFileObject(client.basedir, objKey, imgCreatedAt, nil)
		os.Remove(filePath)
		called := false
		client.handleFile(ctx, log, filePath, info, now, deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = true })
		Expect(called).To(Equal(false))
	})

	It("ListObjectByPrefix lists the correct object without a leading slash", func() {
		_, _ = createFileObject(client.basedir, "dir/other/file", now, nil)
		_, _ = createFileObject(client.basedir, "dir/other/file2", now, nil)
		_, _ = createFileObject(client.basedir, "dir/file", now, nil)
		_, _ = createFileObject(client.basedir, "dir2/file", now, nil)

		var paths []string
		var err error
		containsObj := func(obj string) bool {
			for _, o := range paths {
				if obj == o {
					return true
				}
			}
			return false
		}

		paths, err = client.ListObjectsByPrefix(ctx, "dir/other")
		Expect(err).NotTo(HaveOccurred())
		Expect(len(paths)).To(Equal(2))
		Expect(containsObj("dir/other/file")).To(BeTrue(), "file list %v does not contain \"dir/other/file\"", paths)
		Expect(containsObj("dir/other/file2")).To(BeTrue(), "file list %v does not contain \"dir/other/file2\"", paths)

		paths, err = client.ListObjectsByPrefix(ctx, "dir2")
		Expect(err).NotTo(HaveOccurred())
		Expect(len(paths)).To(Equal(1))
		Expect(paths[0]).To(Equal("dir2/file"))

		paths, err = client.ListObjectsByPrefix(ctx, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(len(paths)).To(Equal(4))

		Expect(containsObj("dir/other/file")).To(BeTrue(), "file list %v does not contain \"dir/other/file\"", paths)
		Expect(containsObj("dir/other/file2")).To(BeTrue(), "file list %v does not contain \"dir/other/file2\"", paths)
		Expect(containsObj("dir/file")).To(BeTrue(), "file list %v does not contain \"dir/file\"", paths)
		Expect(containsObj("dir2/file")).To(BeTrue(), "file list %v does not contain \"dir2/file\"", paths)
	})

	Context("Metadata", func() {

		containsMetadataEntry := func(objects []ObjectInfo, metadata map[string]string) bool {
			for _, object := range objects {
				if len(object.Metadata) != len(metadata) {
					return false
				}
				for key, value := range object.Metadata {
					if val, ok := metadata[key]; ok && val == value {
						return true
					}
				}
			}
			return false
		}

		validateListObjectsByPrefix := func() {
			objects, err := client.ListObjectsByPrefixWithMetadata(ctx, "dir/other")
			Expect(err).NotTo(HaveOccurred())
			Expect(len(objects)).To(Equal(2))
			Expect(containsMetadataEntry(objects, map[string]string{
				"key1": "bar",
				"key2": "foo",
			})).To(BeTrue())
			Expect(containsMetadataEntry(objects, map[string]string{
				"key3": "bar2",
				"key4": "foo2",
			})).To(BeTrue())
			Expect(containsMetadataEntry(objects, map[string]string{
				"key5": "fake",
				"key6": "fabricated",
			})).To(BeFalse())
			Expect(containsMetadataEntry(objects, map[string]string{
				"key3": "bar",
				"key5": "foo",
			})).To(BeFalse())
		}

		It("ListObjectsByPrefix returns metadata about objects", func() {
			createFileObject(client.basedir, fmt.Sprintf("dir/other/%s", uuid.New().String()), now, map[string]string{
				"Key1": "bar",
				"key2": "foo",
			})
			createFileObject(client.basedir, fmt.Sprintf("dir/other/%s", uuid.New().String()), now, map[string]string{
				"key3": "bar2",
				"Key4": "foo2",
			})
			validateListObjectsByPrefix()
		})

		Context("Upload", func() {

			var (
				file1Path         string
				file2Path         string
				tempFileDirectory string
			)

			BeforeEach(func() {
				var err error
				tempFileDirectory, err = os.MkdirTemp(os.TempDir(), fmt.Sprintf("manifest-test-%s", uuid.NewString()))
				Expect(err).ToNot(HaveOccurred())
				file1Path, _ = createFileObject(tempFileDirectory, uuid.NewString(), now, nil)
				file2Path, _ = createFileObject(tempFileDirectory, uuid.NewString(), now, nil)
			})

			AfterEach(func() {
				Expect(os.Remove(file1Path)).To(BeNil())
				Expect(os.Remove(file2Path)).To(BeNil())
				Expect(os.Remove(tempFileDirectory)).To(BeNil())
			})

			It("Upload stores metadata about objects", func() {
				dataStr := "hello world"
				err := client.UploadWithMetadata(ctx, []byte(dataStr), "dir/other/file1", map[string]string{
					"Key1": "bar",
					"key2": "foo",
				})
				Expect(err).Should(BeNil())
				err = client.UploadWithMetadata(ctx, []byte(dataStr), "dir/other/file2", map[string]string{
					"key3": "bar2",
					"Key4": "foo2",
				})
				Expect(err).Should(BeNil())
				validateListObjectsByPrefix()
			})

			It("UploadFile stores metadata about objects", func() {
				Expect(client.UploadFileWithMetadata(ctx, file1Path, "dir/other/file1", map[string]string{
					"Key1": "bar",
					"key2": "foo",
				})).To(BeNil())
				Expect(client.UploadFileWithMetadata(ctx, file2Path, "dir/other/file2", map[string]string{
					"key3": "bar2",
					"Key4": "foo2",
				})).To(BeNil())
				validateListObjectsByPrefix()
			})

			It("UploadStream stores metadata about objects", func() {
				fileReader, err := os.Open(file1Path)
				Expect(err).Should(BeNil())
				err = client.UploadStreamWithMetadata(ctx, fileReader, "dir/other/file1", map[string]string{
					"Key1": "bar",
					"key2": "foo",
				})
				Expect(err).Should(BeNil())
				fileReader.Close()

				fileReader, err = os.Open(file2Path)
				Expect(err).Should(BeNil())
				err = client.UploadStreamWithMetadata(ctx, fileReader, "dir/other/file2", map[string]string{
					"key3": "bar2",
					"Key4": "foo2",
				})
				Expect(err).Should(BeNil())
				fileReader.Close()

				validateListObjectsByPrefix()
			})
		})
	})

	AfterEach(func() {
		os.RemoveAll(baseDir)
	})
})

func setxattr(path string, name string, data []byte, flags int) error {
	key := strings.ToLower(fmt.Sprintf("user.%s", name))
	return unix.Setxattr(path, key, data, flags)
}

func createFileObject(baseDir, objKey string, imgCreatedAt time.Time, metadata map[string]string) (string, os.FileInfo) {
	filePath := filepath.Join(baseDir, objKey)
	Expect(os.MkdirAll(filepath.Dir(filePath), 0755)).To(Succeed())
	err := os.WriteFile(filePath, []byte("Hello world"), 0600)
	Expect(err).Should(BeNil())
	err = os.Chtimes(filePath, imgCreatedAt, imgCreatedAt)
	Expect(err).Should(BeNil())
	info, err := os.Stat(filePath)
	Expect(err).Should(BeNil())
	for k, v := range metadata {
		err := setxattr(filePath, k, []byte(v), 0)
		Expect(err).Should(BeNil())
	}
	return filePath, info
}
