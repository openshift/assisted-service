package app

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
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
func WithHealthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
