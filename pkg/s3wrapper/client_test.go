package s3wrapper

import (
	"context"
	"errors"
	"io/ioutil"
	"strconv"
	"time"

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

	AfterEach(func() {
		ctrl.Finish()
	})
})
