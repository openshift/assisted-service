package stream

import (
	"context"

	"github.com/openshift/assisted-service/pkg/kafka"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=writer_factory.go -package=stream -destination=mock_writer.go

type StreamWriter interface {
	Write(ctx context.Context, key []byte, payload interface{}) error
	Close()
}

type DummyWriter struct{}

func (w *DummyWriter) Write(ctx context.Context, key []byte, value interface{}) error {
	return nil
}

func (w *DummyWriter) Close() {

}

// if streaming disabled this will return a dummy writer. Otherwise will try to return kafka writer
// and fail if any error is encountered
func NewWriter(logger *logrus.Logger, enableNotificationStreaming bool) (StreamWriter, error) {
	writer := &DummyWriter{}
	if !enableNotificationStreaming {
		logger.Info("Initializing event stream dummy writer")
		return writer, nil
	}
	logger.Info("Initializing event stream kafka writer")
	return kafka.NewWriter()
}
