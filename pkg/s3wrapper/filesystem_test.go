package s3wrapper

import (
	"context"
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/sirupsen/logrus"
)

var _ = Describe("s3filesystem", func() {
	var (
		ctx          = context.Background()
		log          = logrus.New()
		deleteTime   time.Duration
		client       *FSClient
		ctrl         *gomock.Controller
		mockVersions *versions.MockHandler
		now          time.Time
		baseDir      string
		dataStr      = "hello world"
		objKey       = "discovery-image-d183c403-d27b-42e1-b0a4-1274ea1a5d77.iso"
		objKey2      = "discovery-image-f318a87b-ba57-4c7e-ae5f-ee562a6d1e8c.iso"
	)
	BeforeEach(func() {
		log.SetOutput(ioutil.Discard)
		var err error
		baseDir, err = ioutil.TempDir("", "test")
		Expect(err).Should(BeNil())

		ctrl = gomock.NewController(GinkgoT())
		mockVersions = versions.NewMockHandler(ctrl)
		client = &FSClient{basedir: baseDir, log: log, versionsHandler: mockVersions}
		deleteTime, _ = time.ParseDuration("60m")
		now, _ = time.Parse(time.RFC3339, "2020-01-01T10:00:00+00:00")
	})
	It("upload_download", func() {
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
		client.handleFile(ctx, log, filePath, info, now, deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = true })
		Expect(called).To(Equal(false))
	})
	It("expire_expired_image", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T08:00:00+00:00") // Two hours ago
		filePath, info := createFileObject(client.basedir, objKey, imgCreatedAt)
		called := false
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
	Context("upload boot files", func() {
		It("all exist", func() {
			for _, fileType := range BootFileExtensions {
				err := ioutil.WriteFile(filepath.Join(baseDir, BootFileTypeToObjectName(client.GetBaseIsoObject(defaultTestOpenShiftVersion), fileType)),
					[]byte("Hello world"), 0600)
				Expect(err).Should(BeNil())
			}

			mockVersions.EXPECT().GetRHCOSImage(defaultTestOpenShiftVersion).Return(defaultTestRhcosURL, nil).Times(1)
			err := client.UploadBootFiles(ctx, defaultTestOpenShiftVersion)
			Expect(err).ToNot(HaveOccurred())
		})
		It("unsupported openshift version", func() {
			unsupportedVersion := "999"
			mockVersions.EXPECT().GetRHCOSImage(unsupportedVersion).Return("", errors.New("unsupported")).Times(1)
			err := client.UploadBootFiles(ctx, unsupportedVersion)
			Expect(err).To(HaveOccurred())
		})
		It("iso exists", func() {
			err := os.MkdirAll(filepath.Join(baseDir, "files/images/pxeboot"), 0755)
			Expect(err).ToNot(HaveOccurred())
			err = ioutil.WriteFile(filepath.Join(baseDir, "files/images/pxeboot/rootfs.img"), []byte("this is rootfs"), 0664)
			Expect(err).ToNot(HaveOccurred())
			err = ioutil.WriteFile(filepath.Join(baseDir, "files/images/pxeboot/initrd.img"), []byte("this is initrd"), 0664)
			Expect(err).ToNot(HaveOccurred())
			err = ioutil.WriteFile(filepath.Join(baseDir, "files/images/pxeboot/vmlinuz"), []byte("this is vmlinuz"), 0664)
			Expect(err).ToNot(HaveOccurred())
			isoPath := filepath.Join(baseDir, client.GetBaseIsoObject(defaultTestOpenShiftVersion))
			cmd := exec.Command("genisoimage", "-rational-rock", "-J", "-joliet-long", "-V", "volumeID", "-o", isoPath, filepath.Join(baseDir, "files"))
			err = cmd.Run()
			Expect(err).ToNot(HaveOccurred())
			err = os.RemoveAll(filepath.Join(baseDir, "files"))
			Expect(err).ToNot(HaveOccurred())

			mockVersions.EXPECT().GetRHCOSImage(defaultTestOpenShiftVersion).Return(defaultTestRhcosURL, nil).Times(1)
			err = client.UploadBootFiles(ctx, defaultTestOpenShiftVersion)
			Expect(err).ToNot(HaveOccurred())

			data, err := ioutil.ReadFile(filepath.Join(baseDir, BootFileTypeToObjectName(client.GetBaseIsoObject(defaultTestOpenShiftVersion), "rootfs.img")))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(data)).To(Equal("this is rootfs"))
		})
	})

	AfterEach(func() {
		os.RemoveAll(baseDir)
	})
})

func createFileObject(baseDir, objKey string, imgCreatedAt time.Time) (string, os.FileInfo) {
	filePath := filepath.Join(baseDir, objKey)
	err := ioutil.WriteFile(filePath, []byte("Hello world"), 0600)
	Expect(err).Should(BeNil())
	err = os.Chtimes(filePath, imgCreatedAt, imgCreatedAt)
	Expect(err).Should(BeNil())
	info, err := os.Stat(filePath)
	Expect(err).Should(BeNil())
	return filePath, info
}
