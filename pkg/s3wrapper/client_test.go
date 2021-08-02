package s3wrapper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/isoeditor"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/sirupsen/logrus"
)

var _ = Describe("s3client", func() {
	var (
		ctx            = context.Background()
		log            = logrus.New()
		ctrl           *gomock.Controller
		deleteTime     time.Duration
		isoUploader    *ISOUploader
		client         *S3Client
		mockAPI        *MockS3API
		publicMockAPI  *MockS3API
		uploader       *MockUploaderAPI
		publicUploader *MockUploaderAPI
		mockVersions   *versions.MockHandler

		bucket       string
		publicBucket string
		now          time.Time
		objKey       = "discovery-image-d183c403-d27b-42e1-b0a4-1274ea1a5d77.iso"
		tagKey       = timestampTagKey
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockAPI = NewMockS3API(ctrl)
		publicMockAPI = NewMockS3API(ctrl)
		uploader = NewMockUploaderAPI(ctrl)
		publicUploader = NewMockUploaderAPI(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		editorFactory := isoeditor.NewFactory(isoeditor.Config{ConcurrentEdits: 10}, nil)
		log.SetOutput(ioutil.Discard)
		bucket = "test"
		publicBucket = "pub-test"
		cfg := Config{S3Bucket: bucket, PublicS3Bucket: publicBucket}
		isoUploader = &ISOUploader{log: log, bucket: bucket, publicBucket: publicBucket, s3client: mockAPI}
		client = &S3Client{log: log, session: nil, client: mockAPI, publicClient: publicMockAPI, uploader: uploader,
			publicUploader: publicUploader, cfg: &cfg, isoUploader: isoUploader, versionsHandler: mockVersions,
			isoEditorFactory: editorFactory}
		deleteTime, _ = time.ParseDuration("60m")
		now, _ = time.Parse(time.RFC3339, "2020-01-01T10:00:00+00:00")
	})
	It("not_expired_image_not_reused", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T09:30:00+00:00") // 30 minutes ago
		obj := s3.Object{Key: &objKey, LastModified: &imgCreatedAt}
		called := false
		client.handleObject(ctx, log, &obj, now, deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = true })
		Expect(called).To(Equal(false))
	})
	It("expired_image_not_reused", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T08:00:00+00:00") // Two hours ago
		obj := s3.Object{Key: &objKey, LastModified: &imgCreatedAt}
		taggingInput := s3.GetObjectTaggingInput{Bucket: &bucket, Key: &objKey}
		tagSet := []*s3.Tag{}
		taggingOutput := s3.GetObjectTaggingOutput{TagSet: tagSet}
		mockAPI.EXPECT().GetObjectTagging(&taggingInput).Return(&taggingOutput, nil)
		deleteInput := s3.DeleteObjectInput{Bucket: &bucket, Key: &objKey}
		mockAPI.EXPECT().DeleteObject(&deleteInput).Return(nil, nil)
		called := false
		client.handleObject(ctx, log, &obj, now, deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = true })
		Expect(called).To(Equal(true))
	})
	It("not_expired_image_reused", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T08:00:00+00:00") // Two hours ago
		durationToAdd, _ := time.ParseDuration("90m")
		unixTime := imgCreatedAt.Add(durationToAdd).Unix() // Tag is now half an hour ago
		obj := s3.Object{Key: &objKey, LastModified: &imgCreatedAt}
		taggingInput := s3.GetObjectTaggingInput{Bucket: &bucket, Key: &objKey}
		tagValue := strconv.Itoa(int(unixTime))
		tag := s3.Tag{Key: &tagKey, Value: &tagValue}
		tagSet := []*s3.Tag{&tag}
		taggingOutput := s3.GetObjectTaggingOutput{TagSet: tagSet}
		mockAPI.EXPECT().GetObjectTagging(&taggingInput).Return(&taggingOutput, nil)
		called := false
		client.handleObject(ctx, log, &obj, now, deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = true })
		Expect(called).To(Equal(false))
	})
	It("expired_image_reused", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T07:00:00+00:00") // Three hours ago
		durationToAdd, _ := time.ParseDuration("90m")
		unixTime := imgCreatedAt.Add(durationToAdd).Unix() // Tag is now 1.5 hours ago
		obj := s3.Object{Key: &objKey, LastModified: &imgCreatedAt}
		taggingInput := s3.GetObjectTaggingInput{Bucket: &bucket, Key: &objKey}
		tagValue := strconv.Itoa(int(unixTime))
		tag := s3.Tag{Key: &tagKey, Value: &tagValue}
		tagSet := []*s3.Tag{&tag}
		taggingOutput := s3.GetObjectTaggingOutput{TagSet: tagSet}
		mockAPI.EXPECT().GetObjectTagging(&taggingInput).Return(&taggingOutput, nil)
		deleteInput := s3.DeleteObjectInput{Bucket: &bucket, Key: &objKey}
		mockAPI.EXPECT().DeleteObject(&deleteInput).Return(nil, nil)
		called := false
		client.handleObject(ctx, log, &obj, now, deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = true })
		Expect(called).To(Equal(true))
	})
	It("expired_image_deletion_failed", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T08:00:00+00:00") // Two hours ago
		unixTime := imgCreatedAt.Unix()                                          // Tag is also two hours ago
		obj := s3.Object{Key: &objKey, LastModified: &imgCreatedAt}
		taggingInput := s3.GetObjectTaggingInput{Bucket: &bucket, Key: &objKey}
		tagValue := strconv.Itoa(int(unixTime))
		tag := s3.Tag{Key: &tagKey, Value: &tagValue}
		tagSet := []*s3.Tag{&tag}
		taggingOutput := s3.GetObjectTaggingOutput{TagSet: tagSet}
		mockAPI.EXPECT().GetObjectTagging(&taggingInput).Return(&taggingOutput, nil)
		deleteInput := s3.DeleteObjectInput{Bucket: &bucket, Key: &objKey}
		mockAPI.EXPECT().DeleteObject(&deleteInput).Return(nil, awserr.New("UnknownError", "UnknownError", errors.New("UnknownError")))
		called := false
		client.handleObject(ctx, log, &obj, now, deleteTime, func(ctx context.Context, log logrus.FieldLogger, objectName string) { called = true })
		Expect(called).To(Equal(false))
	})
	Context("upload iso", func() {
		success := func(hexBytes []byte, baseISOSize, areaOffset, areaLength int64, cached bool) {
			uploadID := "12345"
			destObjName := "object-prefix.iso"
			copySource := fmt.Sprintf("/%s/%s", publicBucket, defaultTestRhcosObject)

			mockAPI.EXPECT().HeadObject(&s3.HeadObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject)}).
				Return(&s3.HeadObjectOutput{ETag: aws.String("abcdefg"), ContentLength: aws.Int64(baseISOSize)}, nil)
			if !cached {
				mockAPI.EXPECT().GetObject(&s3.GetObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject),
					Range: aws.String("bytes=32744-32767")}).
					Return(&s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(hexBytes))}, nil)
				mockAPI.EXPECT().GetObject(&s3.GetObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject),
					Range: aws.String(fmt.Sprintf("bytes=%d-%d", areaOffset, areaOffset+minimumPartSizeBytes-1))}).
					Return(&s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(make([]byte, 100)))}, nil)
			}
			mockAPI.EXPECT().CreateMultipartUploadWithContext(gomock.Any(), &s3.CreateMultipartUploadInput{Bucket: &bucket, Key: aws.String(destObjName)}).
				Return(&s3.CreateMultipartUploadOutput{UploadId: aws.String(uploadID)}, nil)
			partCounter := int64(1)
			byteCounter := int64(0)
			var byteRange string
			for byteCounter < areaOffset {
				if (byteCounter+copyPartChunkSizeBytes > areaOffset-1) || (byteCounter+copyPartChunkSizeBytes+minimumPartSizeBytes > areaOffset-1) {
					byteRange = fmt.Sprintf("bytes=%d-%d", byteCounter, areaOffset-1)
					byteCounter = areaOffset
				} else {
					byteRange = fmt.Sprintf("bytes=%d-%d", byteCounter, byteCounter+copyPartChunkSizeBytes-1)
					byteCounter += copyPartChunkSizeBytes
				}
				mockAPI.EXPECT().UploadPartCopyWithContext(gomock.Any(), &s3.UploadPartCopyInput{Bucket: &bucket, Key: aws.String(destObjName), PartNumber: aws.Int64(partCounter),
					CopySource: aws.String(copySource), CopySourceRange: aws.String(byteRange), UploadId: aws.String(uploadID)}).
					Return(&s3.UploadPartCopyOutput{CopyPartResult: &s3.CopyPartResult{ETag: aws.String(fmt.Sprintf("etag%d", partCounter))}}, nil)
				partCounter++
			}
			mockAPI.EXPECT().UploadPart(gomock.Any()).Return(&s3.UploadPartOutput{ETag: aws.String(fmt.Sprintf("etag%d", partCounter))}, nil)
			partCounter++
			byteCounter = areaOffset + minimumPartSizeBytes
			for byteCounter < baseISOSize {
				if (byteCounter+copyPartChunkSizeBytes > baseISOSize-1) || (byteCounter+copyPartChunkSizeBytes+minimumPartSizeBytes > baseISOSize-1) {
					byteRange = fmt.Sprintf("bytes=%d-%d", byteCounter, baseISOSize-1)
					byteCounter = baseISOSize
				} else {
					byteRange = fmt.Sprintf("bytes=%d-%d", byteCounter, byteCounter+copyPartChunkSizeBytes-1)
					byteCounter += copyPartChunkSizeBytes
				}
				mockAPI.EXPECT().UploadPartCopyWithContext(gomock.Any(), &s3.UploadPartCopyInput{Bucket: &bucket, Key: aws.String(destObjName), PartNumber: aws.Int64(partCounter),
					CopySource: aws.String(copySource), CopySourceRange: aws.String(byteRange), UploadId: aws.String(uploadID)}).
					Return(&s3.UploadPartCopyOutput{CopyPartResult: &s3.CopyPartResult{ETag: aws.String(fmt.Sprintf("etag%d", partCounter))}}, nil)
				partCounter++
			}

			var comp []*s3.CompletedPart
			for i := int64(1); i < partCounter; i++ {
				comp = append(comp, &s3.CompletedPart{ETag: aws.String(fmt.Sprintf("etag%d", i)), PartNumber: aws.Int64(i)})
			}
			mockAPI.EXPECT().CompleteMultipartUploadWithContext(gomock.Any(), &s3.CompleteMultipartUploadInput{
				Bucket: &bucket, Key: aws.String(destObjName), UploadId: &uploadID, MultipartUpload: &s3.CompletedMultipartUpload{Parts: comp},
			}).Return(nil, nil)

			err := client.UploadISO(ctx, "ignition", defaultTestRhcosObject, "object-prefix")
			Expect(err).To(BeNil())
		}
		It("upload_iso_good_flow_v1", func() {
			// Taken from hex dump of ISO
			hexBytes := []byte{0x63, 0x6f, 0x72, 0x65, 0x69, 0x73, 0x6f, 0x2b, // coreiso+
				0x15, 0x9b, 0xac, 0x37, 0x00, 0x00, 0x00, 0x00, // offset = 934058773
				0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00} // length = 262144
			success(hexBytes, int64(944766976), int64(934058773), int64(262144), false)
			success(hexBytes, int64(944766976), int64(934058773), int64(262144), true)
		})
		It("upload_iso_good_flow_v2", func() {
			// Taken from hex dump of ISO
			hexBytes := []byte{0x63, 0x6f, 0x72, 0x65, 0x69, 0x73, 0x6f, 0x2b, // coreiso+
				0x00, 0xb0, 0x7e, 0x00, 0x00, 0x00, 0x00, 0x00, // offset = 8302592
				0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00} // length = 262144
			success(hexBytes, int64(962592768), int64(8302592), int64(262144), false)
			success(hexBytes, int64(962592768), int64(8302592), int64(262144), true)
		})
		It("upload_iso_upload_failure", func() {
			// Taken from hex dump of ISO
			hexBytes := []byte{0x63, 0x6f, 0x72, 0x65, 0x69, 0x73, 0x6f, 0x2b, // coreiso+
				0x00, 0xb0, 0x7e, 0x00, 0x00, 0x00, 0x00, 0x00, // offset = 8302592
				0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00} // length = 262144
			baseISOSize := int64(962592768)
			uploadID := "12345"
			destObjName := "object-prefix.iso"
			copySource := fmt.Sprintf("/%s/%s", publicBucket, defaultTestRhcosObject)

			mockAPI.EXPECT().HeadObject(&s3.HeadObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject)}).
				Return(&s3.HeadObjectOutput{ETag: aws.String("abcdefg"), ContentLength: aws.Int64(baseISOSize)}, nil)
			mockAPI.EXPECT().GetObject(&s3.GetObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject), Range: aws.String("bytes=32744-32767")}).
				Return(&s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(hexBytes))}, nil)
			mockAPI.EXPECT().GetObject(&s3.GetObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject), Range: aws.String("bytes=8302592-13545471")}).
				Return(&s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(make([]byte, 100)))}, nil)
			mockAPI.EXPECT().CreateMultipartUploadWithContext(gomock.Any(), &s3.CreateMultipartUploadInput{Bucket: &bucket, Key: aws.String(destObjName)}).
				Return(&s3.CreateMultipartUploadOutput{UploadId: aws.String(uploadID)}, nil)
			mockAPI.EXPECT().UploadPartCopyWithContext(gomock.Any(), &s3.UploadPartCopyInput{Bucket: &bucket, Key: aws.String(destObjName), PartNumber: aws.Int64(1),
				CopySource: aws.String(copySource), CopySourceRange: aws.String("bytes=0-8302591"), UploadId: aws.String(uploadID)}).
				Return(&s3.UploadPartCopyOutput{CopyPartResult: &s3.CopyPartResult{ETag: aws.String("etag")}}, errors.New("failed"))
			mockAPI.EXPECT().UploadPartCopyWithContext(gomock.Any(), gomock.Any()).
				Return(&s3.UploadPartCopyOutput{CopyPartResult: &s3.CopyPartResult{ETag: aws.String("etagfoo")}}, nil).AnyTimes()
			mockAPI.EXPECT().UploadPart(gomock.Any()).Return(&s3.UploadPartOutput{ETag: aws.String("etagbar")}, nil).AnyTimes()
			mockAPI.EXPECT().AbortMultipartUploadWithContext(gomock.Any(), &s3.AbortMultipartUploadInput{Bucket: &bucket, Key: aws.String(destObjName), UploadId: aws.String(uploadID)})

			err := client.UploadISO(ctx, "ignition", defaultTestRhcosObject, "object-prefix")
			Expect(err).To(HaveOccurred())
		})

		It("cancel context", func() {
			canceledCtx, cancel := context.WithCancel(context.Background())
			// Taken from hex dump of ISO
			hexBytes := []byte{0x63, 0x6f, 0x72, 0x65, 0x69, 0x73, 0x6f, 0x2b, // coreiso+
				0x00, 0xb0, 0x7e, 0x00, 0x00, 0x00, 0x00, 0x00, // offset = 8302592
				0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00} // length = 262144
			baseISOSize := int64(962592768)
			uploadID := "12345"
			destObjName := "object-prefix.iso"
			copySource := fmt.Sprintf("/%s/%s", publicBucket, defaultTestRhcosObject)

			mockAPI.EXPECT().HeadObject(&s3.HeadObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject)}).
				Return(&s3.HeadObjectOutput{ETag: aws.String("abcdefg"), ContentLength: aws.Int64(baseISOSize)}, nil)
			mockAPI.EXPECT().GetObject(&s3.GetObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject), Range: aws.String("bytes=32744-32767")}).
				Return(&s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(hexBytes))}, nil)
			mockAPI.EXPECT().GetObject(&s3.GetObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject), Range: aws.String("bytes=8302592-13545471")}).
				Return(&s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(make([]byte, 100)))}, nil)
			mockAPI.EXPECT().CreateMultipartUploadWithContext(gomock.Any(), &s3.CreateMultipartUploadInput{Bucket: &bucket, Key: aws.String(destObjName)}).
				Return(&s3.CreateMultipartUploadOutput{UploadId: aws.String(uploadID)}, nil)
			mockAPI.EXPECT().UploadPartCopyWithContext(gomock.Any(), &s3.UploadPartCopyInput{Bucket: &bucket, Key: aws.String(destObjName), PartNumber: aws.Int64(1),
				CopySource: aws.String(copySource), CopySourceRange: aws.String("bytes=0-8302591"), UploadId: aws.String(uploadID)}).
				DoAndReturn(func(args ...interface{}) (*s3.UploadPartCopyOutput, error) {
					cancel()
					return &s3.UploadPartCopyOutput{CopyPartResult: &s3.CopyPartResult{ETag: aws.String("etag")}}, errors.New("failed")
				})
			mockAPI.EXPECT().UploadPartCopyWithContext(gomock.Any(), gomock.Any()).
				Return(&s3.UploadPartCopyOutput{CopyPartResult: &s3.CopyPartResult{ETag: aws.String("etagfoo")}}, nil).AnyTimes()
			mockAPI.EXPECT().UploadPart(gomock.Any()).Return(&s3.UploadPartOutput{ETag: aws.String("etagbar")}, nil).AnyTimes()
			// validate that the context that is being used is not the canceled context
			mockAPI.EXPECT().AbortMultipartUploadWithContext(gomock.Not(canceledCtx), &s3.AbortMultipartUploadInput{Bucket: &bucket, Key: aws.String(destObjName), UploadId: aws.String(uploadID)})

			err := client.UploadISO(canceledCtx, "ignition", defaultTestRhcosObject, "object-prefix")
			Expect(err).To(HaveOccurred())
		})
		It("upload_iso_ignition_generate_failure", func() {
			// Taken from hex dump of ISO
			hexBytes := []byte{0x63, 0x6f, 0x72, 0x65, 0x69, 0x73, 0x6f, 0x2b, // coreiso+
				0x00, 0xb0, 0x7e, 0x00, 0x00, 0x00, 0x00, 0x00, // offset = 8302592
				0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00} // length = 262144
			baseISOSize := int64(962592768)
			uploadID := "12345"
			destObjName := "object-prefix.iso"

			mockAPI.EXPECT().HeadObject(&s3.HeadObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject)}).
				Return(&s3.HeadObjectOutput{ETag: aws.String("abcdefg"), ContentLength: aws.Int64(baseISOSize)}, nil)
			mockAPI.EXPECT().GetObject(&s3.GetObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject), Range: aws.String("bytes=32744-32767")}).
				Return(&s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(hexBytes))}, nil)
			mockAPI.EXPECT().GetObject(&s3.GetObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject), Range: aws.String("bytes=8302592-13545471")}).
				Return(&s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(make([]byte, 100)))}, nil)
			mockAPI.EXPECT().CreateMultipartUploadWithContext(gomock.Any(), &s3.CreateMultipartUploadInput{Bucket: &bucket, Key: aws.String(destObjName)}).
				Return(&s3.CreateMultipartUploadOutput{UploadId: aws.String(uploadID)}, nil)
			mockAPI.EXPECT().UploadPartCopyWithContext(gomock.Any(), gomock.Any()).
				Return(&s3.UploadPartCopyOutput{CopyPartResult: &s3.CopyPartResult{ETag: aws.String("etagfoo")}}, nil).AnyTimes()
			mockAPI.EXPECT().UploadPart(gomock.Any()).Return(&s3.UploadPartOutput{ETag: aws.String("etagbar")}, nil).
				Return(&s3.UploadPartOutput{ETag: aws.String("etag")}, errors.New("failed"))
			mockAPI.EXPECT().AbortMultipartUploadWithContext(gomock.Any(), &s3.AbortMultipartUploadInput{Bucket: &bucket, Key: aws.String(destObjName), UploadId: aws.String(uploadID)})

			err := client.UploadISO(ctx, "ignition", defaultTestRhcosObject, "object-prefix")
			Expect(err).To(HaveOccurred())
		})
	})
	Context("upload isos", func() {
		It("all exist", func() {
			publicMockAPI.EXPECT().HeadObject(&s3.HeadObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObjectMinimal)}).
				Return(&s3.HeadObjectOutput{}, nil)
			publicMockAPI.EXPECT().HeadObject(&s3.HeadObjectInput{Bucket: &publicBucket, Key: aws.String(defaultTestRhcosObject)}).
				Return(&s3.HeadObjectOutput{}, nil)
			mockVersions.EXPECT().GetRHCOSImage(defaultTestOpenShiftVersion, defaultTestCpuArchitecture).Return(defaultTestRhcosURL, nil).Times(1)

			// Called once for GetBaseIsoObject and once for GetMinimalIsoObjectName
			mockVersions.EXPECT().GetRHCOSVersion(defaultTestOpenShiftVersion, defaultTestCpuArchitecture).Return(defaultTestRhcosVersion, nil).Times(2)

			err := client.UploadISOs(ctx, defaultTestOpenShiftVersion, defaultTestCpuArchitecture, true)
			Expect(err).ToNot(HaveOccurred())
		})
		It("unsupported openshift version", func() {
			unsupportedVersion := "999"
			mockVersions.EXPECT().GetRHCOSImage(unsupportedVersion, defaultTestCpuArchitecture).Return("", errors.New("unsupported")).Times(1)
			err := client.UploadISOs(ctx, unsupportedVersion, defaultTestCpuArchitecture, false)
			Expect(err).To(HaveOccurred())
		})
		It("missing isos", func() {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				filesDir, err := ioutil.TempDir("", "isotest")
				Expect(err).ToNot(HaveOccurred())
				err = os.MkdirAll(filepath.Join(filesDir, "files/images/pxeboot"), 0755)
				Expect(err).ToNot(HaveOccurred())
				err = ioutil.WriteFile(filepath.Join(filesDir, "files/images/pxeboot/rootfs.img"), []byte("this is rootfs"), 0600)
				Expect(err).ToNot(HaveOccurred())
				err = os.MkdirAll(filepath.Join(filesDir, "files/EFI/redhat"), 0755)
				Expect(err).ToNot(HaveOccurred())
				err = ioutil.WriteFile(filepath.Join(filesDir, "files/EFI/redhat/grub.cfg"), []byte(" linux /images/pxeboot/vmlinuz"), 0600)
				Expect(err).ToNot(HaveOccurred())
				err = os.MkdirAll(filepath.Join(filesDir, "files/isolinux"), 0755)
				Expect(err).ToNot(HaveOccurred())
				err = ioutil.WriteFile(filepath.Join(filesDir, "files/isolinux/isolinux.cfg"), []byte(" append initrd=/images/pxeboot/initrd.img"), 0600)
				Expect(err).ToNot(HaveOccurred())
				err = ioutil.WriteFile(filepath.Join(filesDir, "files/images/assisted_installer_custom.img"), make([]byte, isoeditor.RamDiskPaddingLength), 0600)
				Expect(err).ToNot(HaveOccurred())
				err = ioutil.WriteFile(filepath.Join(filesDir, "files/images/ignition.img"), make([]byte, isoeditor.IgnitionPaddingLength), 0600)
				Expect(err).ToNot(HaveOccurred())
				isoPath := filepath.Join(filesDir, "file.iso")
				cmd := exec.Command("genisoimage", "-rational-rock", "-J", "-joliet-long", "-V", "volumeID", "-o", isoPath, filepath.Join(filesDir, "files"))
				err = cmd.Run()
				Expect(err).ToNot(HaveOccurred())
				file, err := os.Open(isoPath)
				Expect(err).ToNot(HaveOccurred())
				defer file.Close()
				_, err = io.Copy(w, file)
				Expect(err).ToNot(HaveOccurred())
			}))
			defer ts.Close()

			publicMockAPI.EXPECT().HeadObject(&s3.HeadObjectInput{
				Bucket: &publicBucket,
				Key:    aws.String(defaultTestRhcosObject)}).
				Return(nil, awserr.New("NotFound", "NotFound", errors.New("NotFound")))
			publicUploader.EXPECT().Upload(gomock.Any()).Return(nil, nil).Times(2)

			// Should upload version file
			uploader.EXPECT().Upload(gomock.Any()).Return(nil, nil).Times(1)
			mockVersions.EXPECT().GetRHCOSRootFS(defaultTestOpenShiftVersion, defaultTestCpuArchitecture).Return("https://example.com/rootfs/url", nil)

			err := client.uploadISOs(ctx, defaultTestRhcosObject, defaultTestRhcosObjectMinimal, ts.URL, defaultTestOpenShiftVersion, defaultTestCpuArchitecture, false)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("createBucket", func() {
		It("creates the bucket if it doesn't exist", func() {
			mockAPI.EXPECT().HeadBucket(gomock.Any()).Return(nil, awserr.New("NotFound", "NotFound", errors.New("NotFound")))
			mockAPI.EXPECT().CreateBucket(gomock.Any()).Return(&s3.CreateBucketOutput{}, nil)
			Expect(client.createBucket(mockAPI, "fooBucket")).To(Succeed())
		})

		It("fails if it fails to create the bucket", func() {
			mockAPI.EXPECT().HeadBucket(gomock.Any()).Return(nil, awserr.New("NotFound", "NotFound", errors.New("NotFound")))
			mockAPI.EXPECT().CreateBucket(gomock.Any()).Return(nil, awserr.New("Unauthorized", "Unauthorized", errors.New("Unauthorized")))
			Expect(client.createBucket(mockAPI, "fooBucket")).NotTo(Succeed())
		})

		It("doesn't attempt to create the bucket if it already exists", func() {
			mockAPI.EXPECT().HeadBucket(gomock.Any()).Return(&s3.HeadBucketOutput{}, nil)
			Expect(client.createBucket(mockAPI, "fooBucket")).To(Succeed())
		})
	})

	AfterEach(func() {
		ctrl.Finish()
	})
})
