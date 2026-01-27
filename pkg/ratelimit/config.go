package ratelimit

import (
	"time"

	"github.com/sirupsen/logrus"
)

// Config holds rate limiting configuration loaded from environment variables.
type Config struct {
	// Enabled controls whether rate limiting is active
	Enabled bool `envconfig:"RATE_LIMIT_ENABLED" default:"true"`

	// Auth endpoint limits (brute force protection - stricter)
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
func (c *Config) CreateMiddleware(log logrus.FieldLogger) *Middleware {
	limiters := c.CreateLimiters()

	return NewMiddleware(MiddlewareConfig{
		Enabled:  c.Enabled,
		Limiters: limiters,
		Log:      log,
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
