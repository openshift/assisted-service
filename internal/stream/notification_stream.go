package stream

import (
	"context"
	"fmt"
	"reflect"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=notification_stream.go -package=stream -destination=mock_notification_stream.go

type Notifier interface {
	Notify(ctx context.Context, notifiable common.Notifiable) error
	Close()
}

type Envelope struct {
	Name     string
	Payload  interface{}
	Metadata interface{}
}

type NotificationStream struct {
	metadata interface{}
	writer   StreamWriter
	log      logrus.FieldLogger
}

func NewNotificationStream(writer StreamWriter, logger logrus.FieldLogger, metadata interface{}) *NotificationStream {
	return &NotificationStream{
		writer:   writer,
		metadata: metadata,
		log:      logger,
	}

}

func (s *NotificationStream) Notify(ctx context.Context, notifiable common.Notifiable) error {
	if s.writer == nil {
		return nil
	}
	if notifiable == nil || reflect.ValueOf(notifiable).IsNil() {
		return fmt.Errorf("trying to notify on nil notifiable")
	}
	key := ""
	clusterID := notifiable.GetClusterID()
	if clusterID != nil {
		key = clusterID.String()
	}

	envelope := &Envelope{
		Name:     notifiable.NotificationType(),
		Payload:  notifiable.Payload(),
		Metadata: s.metadata,
	}

	if err := s.writer.Write(ctx, []byte(key), envelope); err != nil {
		s.log.WithError(err).WithFields(logrus.Fields{
			"type":         notifiable.NotificationType(),
			"cluster_id":   clusterID,
			"infra_env_id": notifiable.GetInfraEnvID(),
			"host_id":      notifiable.GetHostID(),
		}).Warn("failed to stream notification for resource")
		return err
	}
	return nil
}

func (s *NotificationStream) Close() {
	if s.writer != nil {
		s.writer.Close()
	}
}
