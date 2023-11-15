package log

import (
	"context"

	params "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/sirupsen/logrus"
)

// log formats as defined by LOG_FORMAT env variable
const (
	LogFormatText = "text"
	LogFormatJson = "json"
)

type Config struct {
	LogLevel  string `envconfig:"LOG_LEVEL" default:"info"`
	LogFormat string `envconfig:"LOG_FORMAT" default:"text"`
}

// FromContext equip a given logger with values from the given context
func FromContext(ctx context.Context, inner logrus.FieldLogger) logrus.FieldLogger {
	requestID := requestid.FromContext(ctx)
	return requestid.RequestIDLogger(inner, requestID).WithFields(params.GetContextParams(ctx))
}
