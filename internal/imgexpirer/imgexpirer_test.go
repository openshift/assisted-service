package imgexpirer

import (
	"context"
	"io/ioutil"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/filanov/bm-inventory/internal/events"
	"github.com/filanov/bm-inventory/models"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -package imgexpirer -destination mock_s3iface.go github.com/aws/aws-sdk-go/service/s3/s3iface S3API

func TestExpirer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Image expirer tests Suite")
}

var _ = Describe("image_expirer", func() {
	var (
		ctx        = context.Background()
		log        = logrus.New()
		ctrl       *gomock.Controller
		deleteTime time.Duration
		mockAPI    *MockS3API
		mockEvents *events.MockHandler
		bucket     string
		mgr        *Manager
		now        time.Time
		objKey     = "discovery-image-d183c403-d27b-42e1-b0a4-1274ea1a5d77"
		clusterId  = "d183c403-d27b-42e1-b0a4-1274ea1a5d77"
		tagKey     = "create_sec_since_epoch"
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		log.SetOutput(ioutil.Discard)
		bucket = "test"
		mockAPI = NewMockS3API(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		deleteTime, _ = time.ParseDuration("60m")
		mgr = NewManager(log, mockAPI, bucket, deleteTime, mockEvents)
		now, _ = time.Parse(time.RFC3339, "2020-01-01T10:00:00+00:00")
	})
	It("not_expired_image_not_reused", func() {
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T09:30:00+00:00") // 30 minutes ago
		obj := s3.Object{Key: &objKey, LastModified: &imgCreatedAt}
		mgr.handleObject(ctx, log, &obj, now)
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
		mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, models.EventSeverityInfo, "Deleted image from backend because it expired. It may be generated again at any time.", gomock.Any())
		mgr.handleObject(ctx, log, &obj, now)
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
		mgr.handleObject(ctx, log, &obj, now)
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
		mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, models.EventSeverityInfo, "Deleted image from backend because it expired. It may be generated again at any time.", gomock.Any())
		mgr.handleObject(ctx, log, &obj, now)
	})
	It("dummy_image_expires_immediately", func() {
		clusterId = "00000000-0000-0000-0000-000000000000"
		objKey = "discovery-image-00000000-0000-0000-0000-000000000000"
		imgCreatedAt, _ := time.Parse(time.RFC3339, "2020-01-01T08:00:00+00:00") // Two hours ago
		obj := s3.Object{Key: &objKey, LastModified: &imgCreatedAt}
		deleteInput := s3.DeleteObjectInput{Bucket: &bucket, Key: &objKey}
		mockAPI.EXPECT().DeleteObject(&deleteInput).Return(nil, nil)
		mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, models.EventSeverityInfo, "Deleted image from backend because it expired. It may be generated again at any time.", gomock.Any())
		mgr.handleObject(ctx, log, &obj, now)
	})

	AfterEach(func() {
		ctrl.Finish()
	})
})
