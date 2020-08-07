package app

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
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
