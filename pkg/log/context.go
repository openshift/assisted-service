package log

import (
	"context"
	"runtime"
	"strings"

	params "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/sirupsen/logrus"
)

//log formats as defined by LOG_FORMAT env variable
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
	return requestid.RequestIDLogger(inner, requestID).WithFields(getFields(ctx))
}

//values to be added to the decorated log
func getFields(ctx context.Context) logrus.Fields {
	var fields = make(map[string]interface{})
	fields["go-id"] = goid()

	cluster_id := params.GetParam(ctx, params.ClusterId)
	if cluster_id != "" {
		fields[params.ClusterId] = cluster_id
	}

	host_id := params.GetParam(ctx, params.HostId)
	if host_id != "" {
		fields[params.HostId] = host_id
	}
	return fields
}

// get the low-level gorouting id
// This has been taken from:
// https://groups.google.com/d/msg/golang-nuts/Nt0hVV_nqHE/bwndAYvxAAAJ
// This is hacky and should not be used for anything but logging
func goid() string {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	return idField
}
