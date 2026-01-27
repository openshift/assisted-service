package ratelimit

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

// EndpointCategory represents a category of endpoints with shared rate limits.
type EndpointCategory string

const (
	// CategoryAuth covers authentication-related endpoints (stricter limits for brute force protection)
	CategoryAuth EndpointCategory = "auth"
	// CategoryRead covers read-only operations (higher limits)
	CategoryRead EndpointCategory = "read"
	// CategoryWrite covers mutation operations (moderate limits)
	CategoryWrite EndpointCategory = "write"
	// CategoryDownload covers expensive download operations (lower limits)
	CategoryDownload EndpointCategory = "download"
	// CategoryDefault is the fallback category
	CategoryDefault EndpointCategory = "default"
)

// categoryPatterns defines URL patterns for endpoint categorization.
var categoryPatterns = []struct {
	category EndpointCategory
	patterns []*regexp.Regexp
}{
	{
		category: CategoryAuth,
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`/auth/`),
			regexp.MustCompile(`/token`),
			regexp.MustCompile(`/login`),
		},
	},
	{
		category: CategoryDownload,
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`/downloads?/`),
			regexp.MustCompile(`/presigned`),
			regexp.MustCompile(`/iso`),
			regexp.MustCompile(`/discovery-ignition`),
			regexp.MustCompile(`/minimal-initrd`),
		},
	},
}

// Middleware creates HTTP middleware that applies rate limiting.
// It uses the appropriate rate limiter based on the endpoint category.
type Middleware struct {
	limiters map[EndpointCategory]*RateLimiter
	log      logrus.FieldLogger
	enabled  bool
}

// MiddlewareConfig holds configuration for the rate limiting middleware.
type MiddlewareConfig struct {
	Enabled  bool
	Limiters map[EndpointCategory]*RateLimiter
	Log      logrus.FieldLogger
}

// NewMiddleware creates a new rate limiting middleware with the given configuration.
func NewMiddleware(cfg MiddlewareConfig) *Middleware {
	return &Middleware{
		limiters: cfg.Limiters,
		log:      cfg.Log,
		enabled:  cfg.Enabled,
	}
}

// Handler returns an HTTP handler that applies rate limiting.
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip if rate limiting is disabled
		if !m.enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Skip health and metrics endpoints
		if m.shouldSkip(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Determine endpoint category and get appropriate limiter
		category := m.categorizeEndpoint(r)
		limiter := m.getLimiter(category)

		if limiter == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Get client identifier
		clientID := GetClientID(r)

		// Add rate limit headers (RFC 6585 compliant)
		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.0f", float64(limiter.Rate())))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%.0f", limiter.Tokens(clientID)))

		// Check rate limit
		if !limiter.Allow(clientID) {
			m.logRateLimitHit(r, clientID, category)

			// Record metric
			rateLimitHitsTotal.WithLabelValues(string(category), clientTypeFromID(clientID)).Inc()

			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		// Record successful request
		rateLimitRequestsTotal.WithLabelValues(string(category), clientTypeFromID(clientID)).Inc()

		next.ServeHTTP(w, r)
	})
}

// shouldSkip returns true for endpoints that should not be rate limited.
func (m *Middleware) shouldSkip(r *http.Request) bool {
	path := r.URL.Path
	return path == "/health" ||
		path == "/ready" ||
		path == "/metrics" ||
		strings.HasPrefix(path, "/debug/")
}

// categorizeEndpoint determines the rate limit category for a request.
func (m *Middleware) categorizeEndpoint(r *http.Request) EndpointCategory {
	path := r.URL.Path

	// Check specific patterns
	for _, cp := range categoryPatterns {
		for _, pattern := range cp.patterns {
			if pattern.MatchString(path) {
				return cp.category
			}
		}
	}

	// Categorize by HTTP method
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return CategoryRead
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return CategoryWrite
	default:
		return CategoryDefault
	}
}

// getLimiter returns the rate limiter for the given category.
// Falls back to the default limiter if no specific limiter exists.
func (m *Middleware) getLimiter(category EndpointCategory) *RateLimiter {
	if limiter, ok := m.limiters[category]; ok {
		return limiter
	}
	return m.limiters[CategoryDefault]
}

// logRateLimitHit logs when a client is rate limited.
func (m *Middleware) logRateLimitHit(r *http.Request, clientID string, category EndpointCategory) {
	if m.log != nil {
		m.log.WithFields(logrus.Fields{
			"client_id": clientID,
			"category":  category,
			"method":    r.Method,
			"path":      r.URL.Path,
		}).Warn("Rate limit exceeded")
	}
}

// clientTypeFromID extracts the client type (user or ip) from a client ID.
func clientTypeFromID(clientID string) string {
	if strings.HasPrefix(clientID, "user:") {
		return "user"
	}
	return "ip"
}
