package stream_test

import (
	"context"
	"errors"
	"io/ioutil"
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/stream"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Close", func() {
	var (
		writer   *stream.MockStreamWriter
		logger   *logrus.Logger
		ctrl     *gomock.Controller
		metadata map[string]string
	)

	BeforeEach(func() {
		metadata = map[string]string{
			"foo": "bar",
		}
		logger = logrus.New()
		logger.Out = ioutil.Discard
		ctrl = gomock.NewController(GinkgoT())
		writer = stream.NewMockStreamWriter(ctrl)
	})

	It("should close the writer when closing", func() {
		writer.EXPECT().Close().Times(1)
		notificationStream := stream.NewNotificationStream(writer, logger, metadata)
		notificationStream.Close()
	})

	It("should not try to close the writer when closing with nil writer", func() {
		var emptyWriter stream.StreamWriter
		notificationStream := stream.NewNotificationStream(emptyWriter, logger, metadata)
		notificationStream.Close()
	})
})

var _ = Describe("Notify", func() {
	var (
		ctx       = context.Background()
		writer    *stream.MockStreamWriter
		logger    *logrus.Logger
		ctrl      *gomock.Controller
		clusterID strfmt.UUID
		metadata  map[string]string
	)

	BeforeEach(func() {
		metadata = map[string]string{
			"foo": "bar",
		}
		logger = logrus.New()
		logger.Out = ioutil.Discard
		ctrl = gomock.NewController(GinkgoT())
		writer = stream.NewMockStreamWriter(ctrl)
		clusterID = strfmt.UUID(uuid.New().String())

	})
	It("succeeds when underlying writer is empty", func() {
		event := &common.Event{}
		var emptyWriter stream.StreamWriter
		notificationStream := stream.NewNotificationStream(emptyWriter, logger, metadata)
		err := notificationStream.Notify(ctx, event)
		Expect(err).To(BeNil())
	})

	It("should return error when trying to notify about an empty resource", func() {
		var nilResource *common.Event
		writer.EXPECT().Write(
			ctx,
			gomock.Any(),
			gomock.Any(),
		).Times(0)
		notificationStream := stream.NewNotificationStream(writer, logger, metadata)
		err := notificationStream.Notify(ctx, nilResource)
		Expect(err).NotTo(BeNil())
	})

	It("should return error when trying to notify about an empty interface", func() {
		var emptyInterface common.Notifiable
		writer.EXPECT().Write(
			ctx,
			gomock.Any(),
			gomock.Any(),
		).Times(0)
		notificationStream := stream.NewNotificationStream(writer, logger, metadata)
		err := notificationStream.Notify(ctx, emptyInterface)
		Expect(err).NotTo(BeNil())
	})

	It("should write as event type when notifying Event resource", func() {
		resource := models.Event{
			ClusterID: &clusterID,
		}
		notifiable := &common.Event{
			Event: resource,
		}
		expectedValue := &stream.Envelope{
			Name:     common.NotificationTypeEvent,
			Payload:  &resource,
			Metadata: metadata,
		}
		writer.EXPECT().Write(
			ctx,
			[]byte(clusterID.String()),
			expectedValue,
		).Times(1).Return(nil)
		notificationStream := stream.NewNotificationStream(writer, logger, metadata)
		err := notificationStream.Notify(ctx, notifiable)
		Expect(err).To(BeNil())
	})

	It("should write as cluster type when notifying Cluster resource", func() {
		resource := models.Cluster{
			ID: &clusterID,
		}
		notifiable := &common.Cluster{
			Cluster: resource,
		}
		expectedValue := &stream.Envelope{
			Name:     common.NotificationTypeCluster,
			Payload:  &resource,
			Metadata: metadata,
		}
		writer.EXPECT().Write(
			ctx,
			[]byte(clusterID.String()),
			expectedValue,
		).Times(1).Return(nil)
		notificationStream := stream.NewNotificationStream(writer, logger, metadata)
		err := notificationStream.Notify(ctx, notifiable)
		Expect(err).To(BeNil())
	})

	It("should write as host type when notifying host resource", func() {

		resource := models.Host{
			ClusterID: &clusterID,
		}
		notifiable := &common.Host{
			Host: resource,
		}
		expectedValue := &stream.Envelope{
			Name:     common.NotificationTypeHost,
			Payload:  &resource,
			Metadata: metadata,
		}
		writer.EXPECT().Write(
			ctx,
			[]byte(clusterID.String()),
			expectedValue,
		).Times(1).Return(nil)
		notificationStream := stream.NewNotificationStream(writer, logger, metadata)
		err := notificationStream.Notify(ctx, notifiable)
		Expect(err).To(BeNil())
	})

	It("should write as infraenv type when notifying with infraenv resource", func() {

		resource := models.InfraEnv{
			ClusterID: clusterID,
		}
		notifiable := &common.InfraEnv{
			InfraEnv: resource,
		}
		expectedValue := &stream.Envelope{
			Name:     common.NotificationTypeInfraEnv,
			Payload:  &resource,
			Metadata: metadata,
		}
		writer.EXPECT().Write(
			ctx,
			[]byte(clusterID.String()),
			expectedValue,
		).Times(1).Return(nil)
		notificationStream := stream.NewNotificationStream(writer, logger, metadata)
		err := notificationStream.Notify(ctx, notifiable)
		Expect(err).To(BeNil())
	})

	It("should return error when writer returns error", func() {

		resource := models.InfraEnv{
			ClusterID: clusterID,
		}
		notifiable := &common.InfraEnv{
			InfraEnv: resource,
		}
		expectedValue := &stream.Envelope{
			Name:     common.NotificationTypeInfraEnv,
			Payload:  &resource,
			Metadata: metadata,
		}
		writer.EXPECT().Write(
			ctx,
			[]byte(clusterID.String()),
			expectedValue,
		).Times(1).Return(errors.New("something went wrong"))
		notificationStream := stream.NewNotificationStream(writer, logger, metadata)
		err := notificationStream.Notify(ctx, notifiable)
		Expect(err).NotTo(BeNil())
	})
})

func TestNotificationStream(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Notification stream")
}
