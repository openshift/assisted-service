package s3wrapper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"

	"github.com/aws/aws-sdk-go/aws/awserr"

	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("s3client", func() {
	var (
		ctx        = context.Background()
		log        = logrus.New()
		ctrl       *gomock.Controller
		deleteTime time.Duration
		client     *S3Client
		mockAPI    *MockS3API
		bucket     string
		now        time.Time
		objKey     = "discovery-image-d183c403-d27b-42e1-b0a4-1274ea1a5d77.iso"
		tagKey     = timestampTagKey
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockAPI = NewMockS3API(ctrl)
		log.SetOutput(ioutil.Discard)
		bucket = "test"
		cfg := Config{S3Bucket: "test"}
		client = &S3Client{log: log, session: nil, client: mockAPI, cfg: &cfg}
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
		unixTime := imgCreatedAt.Unix()                                          // Tag is also two hours ago
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
		success := func(hexBytes []byte, baseISOSize int64, firstCopyRange, secondCopyRange, thirdCopyRange string) {
			uploadID := "12345"
			destObjName := "object-prefix.iso"
			copySource := fmt.Sprintf("/%s/%s", bucket, BaseObjectName)
			etag1 := "etag1"
			etag2 := "etag2"
			etag3 := "etag3"

			mockAPI.EXPECT().GetObject(&s3.GetObjectInput{Bucket: &bucket, Key: aws.String(BaseObjectName), Range: aws.String("bytes=32744-32767")}).
				Return(&s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(hexBytes))}, nil)
			mockAPI.EXPECT().HeadObject(&s3.HeadObjectInput{Bucket: &bucket, Key: aws.String(BaseObjectName)}).
				Return(&s3.HeadObjectOutput{ContentLength: aws.Int64(baseISOSize)}, nil)
			mockAPI.EXPECT().CreateMultipartUpload(&s3.CreateMultipartUploadInput{Bucket: &bucket, Key: aws.String(destObjName)}).
				Return(&s3.CreateMultipartUploadOutput{UploadId: aws.String(uploadID)}, nil)
			mockAPI.EXPECT().UploadPartCopy(&s3.UploadPartCopyInput{Bucket: &bucket, Key: aws.String(destObjName), PartNumber: aws.Int64(1),
				CopySource: aws.String(copySource), CopySourceRange: aws.String(firstCopyRange), UploadId: aws.String(uploadID)}).
				Return(&s3.UploadPartCopyOutput{CopyPartResult: &s3.CopyPartResult{ETag: aws.String(etag1)}}, nil)
			mockAPI.EXPECT().GetObject(&s3.GetObjectInput{Bucket: &bucket, Key: aws.String(BaseObjectName), Range: aws.String(secondCopyRange)}).
				Return(&s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(make([]byte, 100)))}, nil)
			mockAPI.EXPECT().UploadPart(gomock.Any()).Return(&s3.UploadPartOutput{ETag: aws.String(etag2)}, nil)
			mockAPI.EXPECT().UploadPartCopy(&s3.UploadPartCopyInput{Bucket: &bucket, Key: aws.String(destObjName), PartNumber: aws.Int64(3),
				CopySource: aws.String(copySource), CopySourceRange: aws.String(thirdCopyRange), UploadId: aws.String(uploadID)}).
				Return(&s3.UploadPartCopyOutput{CopyPartResult: &s3.CopyPartResult{ETag: aws.String(etag3)}}, nil)
			mockAPI.EXPECT().CompleteMultipartUpload(&s3.CompleteMultipartUploadInput{Bucket: &bucket, Key: aws.String(destObjName),
				UploadId: aws.String(uploadID), MultipartUpload: &s3.CompletedMultipartUpload{Parts: []*s3.CompletedPart{
					{ETag: aws.String(etag1), PartNumber: aws.Int64(1)},
					{ETag: aws.String(etag2), PartNumber: aws.Int64(2)},
					{ETag: aws.String(etag3), PartNumber: aws.Int64(3)}}}}).Return(nil, nil)

			err := client.UploadISO(ctx, "ignition", "object-prefix")
			Expect(err).To(BeNil())
		}
		It("upload_iso_good_flow_v1", func() {
			// Taken from hex dump of ISO
			hexBytes := []byte{0x63, 0x6f, 0x72, 0x65, 0x69, 0x73, 0x6f, 0x2b, // coreiso+
				0x15, 0x9b, 0xac, 0x37, 0x00, 0x00, 0x00, 0x00, // offset = 934058773
				0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00} // length = 262144
			success(hexBytes, int64(944766976), "bytes=0-934058772", "bytes=934058773-939301652", "bytes=939301653-944766975")
		})
		It("upload_iso_good_flow_v2", func() {
			// Taken from hex dump of ISO
			hexBytes := []byte{0x63, 0x6f, 0x72, 0x65, 0x69, 0x73, 0x6f, 0x2b, // coreiso+
				0x00, 0xb0, 0x7e, 0x00, 0x00, 0x00, 0x00, 0x00, // offset = 8302592
				0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00} // length = 262144
			success(hexBytes, int64(962592768), "bytes=0-8302591", "bytes=8302592-13545471", "bytes=13545472-962592767")
		})
		It("upload_iso_upload_failure", func() {
			// Taken from hex dump of ISO
			hexBytes := []byte{0x63, 0x6f, 0x72, 0x65, 0x69, 0x73, 0x6f, 0x2b, // coreiso+
				0x00, 0xb0, 0x7e, 0x00, 0x00, 0x00, 0x00, 0x00, // offset = 8302592
				0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00} // length = 262144
			baseISOSize := int64(962592768)
			uploadID := "12345"
			destObjName := "object-prefix.iso"
			copySource := fmt.Sprintf("/%s/%s", bucket, BaseObjectName)
			etag1 := "etag1"

			mockAPI.EXPECT().GetObject(&s3.GetObjectInput{Bucket: &bucket, Key: aws.String(BaseObjectName), Range: aws.String("bytes=32744-32767")}).
				Return(&s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(hexBytes))}, nil)
			mockAPI.EXPECT().HeadObject(&s3.HeadObjectInput{Bucket: &bucket, Key: aws.String(BaseObjectName)}).
				Return(&s3.HeadObjectOutput{ContentLength: aws.Int64(baseISOSize)}, nil)
			mockAPI.EXPECT().CreateMultipartUpload(&s3.CreateMultipartUploadInput{Bucket: &bucket, Key: aws.String(destObjName)}).
				Return(&s3.CreateMultipartUploadOutput{UploadId: aws.String(uploadID)}, nil)
			mockAPI.EXPECT().UploadPartCopy(&s3.UploadPartCopyInput{Bucket: &bucket, Key: aws.String(destObjName), PartNumber: aws.Int64(1),
				CopySource: aws.String(copySource), CopySourceRange: aws.String("bytes=0-8302591"), UploadId: aws.String(uploadID)}).
				Return(&s3.UploadPartCopyOutput{CopyPartResult: &s3.CopyPartResult{ETag: aws.String(etag1)}}, nil)
			mockAPI.EXPECT().GetObject(&s3.GetObjectInput{Bucket: &bucket, Key: aws.String(BaseObjectName), Range: aws.String("bytes=8302592-13545471")}).
				Return(&s3.GetObjectOutput{Body: ioutil.NopCloser(bytes.NewReader(make([]byte, 100)))}, errors.New("Failed"))
			mockAPI.EXPECT().AbortMultipartUpload(&s3.AbortMultipartUploadInput{Bucket: &bucket, Key: aws.String(destObjName), UploadId: aws.String(uploadID)})

			err := client.UploadISO(ctx, "ignition", "object-prefix")
			Expect(err).To(HaveOccurred())
		})
	})

	AfterEach(func() {
		ctrl.Finish()
	})
})
