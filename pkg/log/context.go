package log

import (
	"context"
	"net/http"
	"runtime"
	"strings"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/sirupsen/logrus"
)

type contextKey string

//url parameters that should be added as fields to logs
const (
	PARAM          = "PARAM$" //prefix for all params
	clusterIdParam = "cluster_id"
	hostIdParam    = "host_id"
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

	cluster_id := getParam(ctx, clusterIdParam)
	if cluster_id != "" {
		fields[clusterIdParam] = cluster_id
	}

	host_id := getParam(ctx, hostIdParam)
	if host_id != "" {
		fields[hostIdParam] = host_id
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

func paramKey(name string) string {
	return PARAM + name
}

func getParam(ctx context.Context, key string) string {
	val := ctx.Value(contextKey(paramKey(key)))
	if val == nil {
		return ""
	} else {
		return val.(string)
	}
}

//openapi middleware handler that takes URL params from the route and put them
//on the context for further use (mainly by logs). parameters are prefixed to avoid
//conflicts (for example: cluster_id --> PARAM$cluster_id)
func ContextHandler() func(http.Handler) http.Handler {

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			//extract the Route from the request
			mr := middleware.MatchedRouteFrom(r)
			//set the parameters on the request's context
			ctx := r.Context()
			if mr != nil {
				for _, p := range mr.Params {
					ctx = context.WithValue(ctx, contextKey(paramKey(p.Name)), p.Value)
				}
				r = r.WithContext(ctx)
			}

			//pass control to the next handler in the chain
			next.ServeHTTP(w, r)
		})
	}
}
