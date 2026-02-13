package ratelimit

import (
	"time"

	"github.com/sirupsen/logrus"
)

// Config holds rate limiting configuration loaded from environment variables.
type Config struct {
	// Enabled controls whether rate limiting is active.
	// Defaults to false (disabled) for safety in existing deployments.
	// Set to "true" to enable rate limiting in SaaS/multi-tenant environments.
	Enabled bool `envconfig:"RATE_LIMIT_ENABLED" default:"false"`

	// Auth endpoint limits (brute force protection - stricter)
	// The ~5 requests/minute limit (0.1 RPS with burst 5) is intentionally restrictive
	// to prevent brute force attacks. This applies to login attempts, token refresh,
	// and any endpoint that could leak user existence.
	// For legitimate high-volume scenarios (e.g., batch operations), use service
	// accounts with higher limits or implement retry with exponential backoff.
	AuthRPS   float64 `envconfig:"RATE_LIMIT_AUTH_RPS" default:"0.1"`
	AuthBurst int     `envconfig:"RATE_LIMIT_AUTH_BURST" default:"5"`

	// Read endpoint limits (higher limits for read operations)
	ReadRPS   float64 `envconfig:"RATE_LIMIT_READ_RPS" default:"1.67"`
	ReadBurst int     `envconfig:"RATE_LIMIT_READ_BURST" default:"100"`

	// Write endpoint limits (moderate limits for mutations)
	WriteRPS   float64 `envconfig:"RATE_LIMIT_WRITE_RPS" default:"0.33"`
	WriteBurst int     `envconfig:"RATE_LIMIT_WRITE_BURST" default:"20"`

	// Download endpoint limits (lower limits for expensive operations)
	DownloadRPS   float64 `envconfig:"RATE_LIMIT_DOWNLOAD_RPS" default:"0.17"`
	DownloadBurst int     `envconfig:"RATE_LIMIT_DOWNLOAD_BURST" default:"10"`

	// Default endpoint limits (fallback)
	DefaultRPS   float64 `envconfig:"RATE_LIMIT_DEFAULT_RPS" default:"1.67"`
	DefaultBurst int     `envconfig:"RATE_LIMIT_DEFAULT_BURST" default:"100"`

	// CleanupInterval controls how often stale rate limit entries are cleaned up
	CleanupInterval time.Duration `envconfig:"RATE_LIMIT_CLEANUP_INTERVAL" default:"10m"`

	// TrustProxyHeaders controls whether to trust X-Forwarded-For and X-Real-IP headers.
	// Only enable this when running behind a trusted proxy/load balancer.
	// When false (default in production), only r.RemoteAddr is used for client identification.
	TrustProxyHeaders bool `envconfig:"RATE_LIMIT_TRUST_PROXY_HEADERS" default:"false"`

	// TrustedProxies is a comma-separated list of trusted proxy CIDRs or IPs.
	// When TrustProxyHeaders is true, X-Forwarded-For/X-Real-IP headers are only
	// trusted if the immediate peer IP is in this list. Empty means trust all proxies
	// when TrustProxyHeaders is true (for backward compatibility in trusted environments).
	TrustedProxies string `envconfig:"RATE_LIMIT_TRUSTED_PROXIES" default:""`
}

// CategoryConfig holds rate limit settings for a single category.
type CategoryConfig struct {
	RPS   float64
	Burst int
}

// GetCategoryConfig returns the rate limit configuration for each category.
func (c *Config) GetCategoryConfig() map[EndpointCategory]CategoryConfig {
	return map[EndpointCategory]CategoryConfig{
		CategoryAuth: {
			RPS:   c.AuthRPS,
			Burst: c.AuthBurst,
		},
		CategoryRead: {
			RPS:   c.ReadRPS,
			Burst: c.ReadBurst,
		},
		CategoryWrite: {
			RPS:   c.WriteRPS,
			Burst: c.WriteBurst,
		},
		CategoryDownload: {
			RPS:   c.DownloadRPS,
			Burst: c.DownloadBurst,
		},
		CategoryDefault: {
			RPS:   c.DefaultRPS,
			Burst: c.DefaultBurst,
		},
	}
}

// CreateLimiters creates rate limiters for all endpoint categories based on configuration.
func (c *Config) CreateLimiters() map[EndpointCategory]*RateLimiter {
	limiters := make(map[EndpointCategory]*RateLimiter)
	categories := c.GetCategoryConfig()

	for category, cfg := range categories {
		limiters[category] = NewRateLimiter(cfg.RPS, cfg.Burst, c.CleanupInterval)
	}

	return limiters
}

// CreateMiddleware creates a configured rate limiting middleware.
// If rate limiting is disabled, no limiters are created to avoid unnecessary
// background goroutines.
func (c *Config) CreateMiddleware(log logrus.FieldLogger) *Middleware {
	// When rate limiting is disabled, don't create limiters to avoid
	// starting unnecessary background cleanup goroutines
	var limiters map[EndpointCategory]*RateLimiter
	if c.Enabled {
		limiters = c.CreateLimiters()
	}
	proxyTrustConfig := ParseTrustedProxies(c.TrustProxyHeaders, c.TrustedProxies)

	return NewMiddleware(MiddlewareConfig{
		Enabled:          c.Enabled,
		Limiters:         limiters,
		Log:              log,
		ProxyTrustConfig: proxyTrustConfig,
	})
}

// StopLimiters stops all rate limiter cleanup goroutines.
func StopLimiters(limiters map[EndpointCategory]*RateLimiter) {
	for _, limiter := range limiters {
		if limiter != nil {
			limiter.Stop()
		}
	}
}
