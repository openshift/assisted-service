package app

import (
	"bytes"
	"net/http"
	"runtime/pprof"
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

func dumpStacks() string {
	buf := bytes.NewBuffer(nil)
	_ = pprof.Lookup("goroutine").WriteTo(buf, 2)
	return buf.String()
}

// WithHealthMiddleware returns middleware which responds to the /health endpoint
func WithHealthMiddleware(next http.Handler, threads []*thread.Thread, logger logrus.FieldLogger, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health" {
			status := http.StatusOK
			for _, th := range threads {
				if time.Since(th.LastRunStartedAt()) > timeout {
					logger.Errorf("thread %s live probe validation failed, last run timestamp "+
						"is %v, current time %s", th.Name(), th.LastRunStartedAt(), time.Now())
					logger.Errorf("Stacks:\n%s", dumpStacks())
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
