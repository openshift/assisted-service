package app

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/pkg/thread"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
	"github.com/sirupsen/logrus"
)

const (
	ipxeScriptQueryKey   = "file_name"
	ipxeScriptQueryValue = "ipxe-script"
)

var ipxeScriptPattern = regexp.MustCompile(fmt.Sprintf(`^%s/v2/infra-envs/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/downloads/files`, client.DefaultBasePath))

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
func WithHealthMiddleware(next http.Handler, threads []*thread.Thread, logger logrus.FieldLogger, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health" {
			status := http.StatusOK
			for _, th := range threads {
				if time.Since(th.LastRunStartedAt()) > timeout {
					logger.Errorf("thread %s live probe validation failed, last run timestamp "+
						"is %v, current time %s", th.Name(), th.LastRunStartedAt(), time.Now())
					status = http.StatusInternalServerError
					break
				}
			}
			w.WriteHeader(status)
			return
		} else if r.Method == http.MethodGet && r.URL.Path == "/ready" {
			w.WriteHeader(http.StatusOK)
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
			http.MethodHead,
			http.MethodOptions,
		},
		AllowedOrigins: domains,
		AllowedHeaders: []string{
			"Authorization",
			"Content-Type",
			"Severity-Count-Info",
			"Severity-Count-Warning",
			"Severity-Count-Error",
			"Severity-Count-Critical",
			"Event-Count",
		},
		ExposedHeaders: []string{
			"Severity-Count-Info",
			"Severity-Count-Warning",
			"Severity-Count-Error",
			"Severity-Count-Critical",
			"Event-Count",
		},
		MaxAge: int((10 * time.Minute).Seconds()),
	})
	return corsHandler.Handler(handler)
}

func WithIPXEScriptMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check plain HTTP requests to API only
		if strings.HasPrefix(r.URL.Path, client.DefaultBasePath) && r.TLS == nil {
			if r.Method != http.MethodGet {
				http.NotFound(w, r)
				return
			}
			matches := ipxeScriptPattern.FindStringSubmatch(r.URL.Path)
			if matches == nil {
				// Path doesn't match the regexp
				http.NotFound(w, r)
				return
			}
			if !r.URL.Query().Has(ipxeScriptQueryKey) {
				// Missing file name parameter
				http.NotFound(w, r)
				return
			}
			if queryValue := r.URL.Query().Get(ipxeScriptQueryKey); queryValue != ipxeScriptQueryValue {
				// Invalid file name requested
				http.NotFound(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
