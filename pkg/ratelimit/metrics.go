package ratelimit

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// rateLimitHitsTotal counts the number of requests that hit rate limits
	rateLimitHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_rate_limit_hits_total",
			Help: "Total number of requests that exceeded rate limits",
		},
		[]string{"endpoint_category", "client_type"},
	)

	// rateLimitRequestsTotal counts all requests processed by the rate limiter
	rateLimitRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_rate_limit_requests_total",
			Help: "Total number of requests processed by rate limiter",
		},
		[]string{"endpoint_category", "client_type"},
	)
)

func init() {
	prometheus.MustRegister(rateLimitHitsTotal)
	prometheus.MustRegister(rateLimitRequestsTotal)
}

// GetRateLimitHitsTotal returns the rate limit hits counter for testing.
func GetRateLimitHitsTotal() *prometheus.CounterVec {
	return rateLimitHitsTotal
}

// GetRateLimitRequestsTotal returns the rate limit requests counter for testing.
func GetRateLimitRequestsTotal() *prometheus.CounterVec {
	return rateLimitRequestsTotal
}
