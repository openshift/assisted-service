package app

import (
	"net/http"
	"time"

	"github.com/openshift/assisted-service/pkg/thread"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
	"github.com/sirupsen/logrus"
)

// WithMetricsResponderMiddleware Returns middleware which responds to /metrics endpoint with the prometheus metrics
// of the service
func WithMetricsResponderMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/metrics" {
			promhttp.Handler().ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// WithHealthMiddleware returns middleware which responds to the /health endpoint
func WithHealthMiddleware(next http.Handler, threads []*thread.Thread, logger logrus.FieldLogger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health" {
			status := http.StatusOK
			for _, th := range threads {
				if th.LastRunTimestamp().IsZero() && time.Since(th.LastRunTimestamp()).Minutes() > 5 {
					logger.Errorf("thread %s live probe validation failed, last run timestamp "+
						"is %v, current time %s", th.Name(), th.LastRunTimestamp(), time.Now())
					status = http.StatusInternalServerError
					break
				}
			}
			w.WriteHeader(status)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func SetupCORSMiddleware(handler http.Handler, domains []string) http.Handler {
	corsHandler := cors.New(cors.Options{
		Debug: false,
		AllowedMethods: []string{
			http.MethodDelete,
			http.MethodGet,
			http.MethodPatch,
			http.MethodPost,
			http.MethodPut,
		},
		AllowedOrigins: domains,
		AllowedHeaders: []string{
			"Authorization",
			"Content-Type",
		},
		MaxAge: int((10 * time.Minute).Seconds()),
	})
	return corsHandler.Handler(handler)
}
