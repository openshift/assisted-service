package metrics

import (
	"net/http"

	"github.com/sirupsen/logrus"
	"github.com/slok/go-http-metrics/middleware"
)

// To be used as an inner middleware to provide metrics for the endpoints
func WithMatchedRoute(log logrus.FieldLogger) func(http.Handler) http.Handler {
	m := middleware.New(middleware.Config{
		Recorder: NewRecorder(Config{Log: log}),
		Service:  "assisted-installer",
	})

	return func(next http.Handler) http.Handler {
		return Handler(log, m, next)
	}
}
