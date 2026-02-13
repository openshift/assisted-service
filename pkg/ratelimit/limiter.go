package ratelimit

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

// AuthenticatedUserContextKey is the context key used to store the authenticated user ID.
// Authentication middleware should set this value in the request context after validating credentials.
const AuthenticatedUserContextKey contextKey = "authenticated_user_id"

// SetAuthenticatedUser returns a new context with the authenticated user ID set.
// This should be called by authentication middleware after successful authentication.
func SetAuthenticatedUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, AuthenticatedUserContextKey, userID)
}

// GetAuthenticatedUser retrieves the authenticated user ID from the context.
// Returns empty string if no authenticated user is set.
func GetAuthenticatedUser(ctx context.Context) string {
	if userID, ok := ctx.Value(AuthenticatedUserContextKey).(string); ok {
		return userID
	}
	return ""
}

// ProxyTrustConfig holds configuration for trusting proxy headers.
type ProxyTrustConfig struct {
	// TrustProxyHeaders enables trusting X-Forwarded-For and X-Real-IP headers.
	TrustProxyHeaders bool
	// TrustedProxyCIDRs is a list of trusted proxy CIDRs. If empty and TrustProxyHeaders
	// is true, all proxies are trusted (for backward compatibility).
	TrustedProxyCIDRs []*net.IPNet
	// TrustedProxyIPs is a list of trusted proxy IPs (for single IPs without CIDR).
	TrustedProxyIPs map[string]bool
}

// ParseTrustedProxies parses a comma-separated list of CIDRs/IPs into a ProxyTrustConfig.
func ParseTrustedProxies(trustHeaders bool, proxies string) ProxyTrustConfig {
	cfg := ProxyTrustConfig{
		TrustProxyHeaders: trustHeaders,
		TrustedProxyCIDRs: make([]*net.IPNet, 0),
		TrustedProxyIPs:   make(map[string]bool),
	}

	if proxies == "" {
		return cfg
	}

	for _, p := range strings.Split(proxies, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Try parsing as CIDR
		_, cidr, err := net.ParseCIDR(p)
		if err == nil {
			cfg.TrustedProxyCIDRs = append(cfg.TrustedProxyCIDRs, cidr)
			continue
		}

		// Try parsing as IP
		ip := net.ParseIP(p)
		if ip != nil {
			cfg.TrustedProxyIPs[ip.String()] = true
		}
	}

	return cfg
}

// RateLimiter provides per-client rate limiting using a token bucket algorithm.
// Each client (identified by IP address) gets their own rate limiter with
// configurable requests per second and burst capacity. Rate limiting runs
// in the outer middleware chain before authentication for network-edge protection.
type RateLimiter struct {
	visitors    map[string]*visitorState
	mu          sync.RWMutex
	rps         rate.Limit // requests per second
	burst       int        // burst size (max tokens)
	cleanupFreq time.Duration
	stopCleanup chan struct{}
	stopOnce    sync.Once // ensures Stop() is safe to call multiple times
}

// visitorState tracks rate limiting state for a single client
type visitorState struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter creates a new rate limiter with the specified requests per second
// and burst size. The cleanupFreq parameter controls how often stale entries are
// removed from the visitor map.
// Invalid rps (<=0) defaults to 1.0, invalid burst (<1) defaults to 1.
func NewRateLimiter(rps float64, burst int, cleanupFreq time.Duration) *RateLimiter {
	// Validate rps: must be positive to allow requests
	if rps <= 0 {
		rps = 1.0
	}
	// Validate burst: must be at least 1 to allow any requests
	if burst < 1 {
		burst = 1
	}

	rl := &RateLimiter{
		visitors:    make(map[string]*visitorState),
		rps:         rate.Limit(rps),
		burst:       burst,
		cleanupFreq: cleanupFreq,
		stopCleanup: make(chan struct{}),
	}

	// Start background cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// GetLimiter returns the rate limiter for a specific client, creating one if needed.
func (rl *RateLimiter) GetLimiter(clientID string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[clientID]
	if !exists {
		limiter := rate.NewLimiter(rl.rps, rl.burst)
		rl.visitors[clientID] = &visitorState{
			limiter:  limiter,
			lastSeen: time.Now(),
		}
		return limiter
	}

	v.lastSeen = time.Now()
	return v.limiter
}

// Allow checks if the request should be allowed for the given client ID.
// Returns true if the request is within rate limits, false otherwise.
func (rl *RateLimiter) Allow(clientID string) bool {
	return rl.GetLimiter(clientID).Allow()
}

// Tokens returns the current number of available tokens for a client.
func (rl *RateLimiter) Tokens(clientID string) float64 {
	return rl.GetLimiter(clientID).Tokens()
}

// Rate returns the configured rate limit in requests per second.
func (rl *RateLimiter) Rate() rate.Limit {
	return rl.rps
}

// Burst returns the configured burst size.
func (rl *RateLimiter) Burst() int {
	return rl.burst
}

// Stop stops the cleanup goroutine. It is safe to call Stop() multiple times;
// subsequent calls after the first are no-ops.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() {
		close(rl.stopCleanup)
	})
}

// cleanupLoop periodically removes stale visitor entries that haven't been seen
// for a configurable duration.
func (rl *RateLimiter) cleanupLoop() {
	// Guard against non-positive durations which would cause time.NewTicker to panic.
	// If cleanup is disabled (zero/negative interval), simply return and don't run cleanup.
	if rl.cleanupFreq <= 0 {
		return
	}

	ticker := time.NewTicker(rl.cleanupFreq)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCleanup:
			return
		}
	}
}

// cleanup removes visitor entries that haven't been seen recently.
// Entries are removed if they haven't been seen for 3x the cleanup frequency.
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	staleThreshold := time.Now().Add(-3 * rl.cleanupFreq)
	for clientID, v := range rl.visitors {
		if v.lastSeen.Before(staleThreshold) {
			delete(rl.visitors, clientID)
		}
	}
}

// GetClientID extracts a unique client identifier from the HTTP request.
// Rate limiting is applied in the outer middleware chain before authentication,
// so we use IP-based identification for all requests. This provides effective
// DoS protection at the network edge.
// Deprecated: Use GetClientIDWithConfig for proper proxy trust handling.
func GetClientID(r *http.Request) string {
	// Default to not trusting proxy headers for backward compatibility
	return GetClientIDWithConfig(r, ProxyTrustConfig{TrustProxyHeaders: false})
}

// GetClientIDWithConfig extracts a unique client identifier using the provided proxy config.
// If an authenticated user is present in the request context, the user ID is used for per-user
// rate limiting. Otherwise, the client IP is used for per-IP rate limiting.
// This allows authenticated users behind the same NAT to have separate rate limit buckets.
func GetClientIDWithConfig(r *http.Request, cfg ProxyTrustConfig) string {
	// Check for authenticated user in request context first
	// This enables per-user rate limiting for authenticated requests,
	// preventing users behind the same NAT from sharing rate limit buckets.
	if userID := GetAuthenticatedUser(r.Context()); userID != "" {
		return "user:" + userID
	}

	// Fall back to IP-based identification for unauthenticated requests
	return "ip:" + getClientIPWithConfig(r, cfg)
}

// getClientIPWithConfig extracts the client IP address from the request,
// considering X-Forwarded-For and X-Real-IP headers only when proxy headers are trusted
// and the immediate peer is in the trusted proxy list.
//
// Client identification strategy:
//  1. Authenticated requests: Use user/service account ID from auth context
//  2. Unauthenticated requests: Use client IP address
//  3. Behind proxy: Trust X-Forwarded-For only if TrustProxyHeaders is enabled
//     and the immediate peer is in the trusted proxy list
//
// This ensures rate limits apply per-identity rather than per-connection,
// preventing bypass via connection pooling.
func getClientIPWithConfig(r *http.Request, cfg ProxyTrustConfig) string {
	// Extract the immediate peer IP from RemoteAddr
	peerIP := extractIP(r.RemoteAddr)

	// Only trust proxy headers if configured to do so
	if cfg.TrustProxyHeaders && isPeerTrusted(peerIP, cfg) {
		// Check X-Forwarded-For header (may contain multiple IPs)
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Take the first IP (original client), trim spaces
			for i, c := range xff {
				if c == ',' {
					return strings.TrimSpace(xff[:i])
				}
			}
			return strings.TrimSpace(xff)
		}

		// Check X-Real-IP header
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	// Fall back to RemoteAddr
	return peerIP
}

// extractIP extracts just the IP address from an address that may include a port.
func extractIP(addr string) string {
	ip, _, err := net.SplitHostPort(addr)
	if err != nil {
		// May not have a port (e.g., unix socket or already just an IP)
		return addr
	}
	return ip
}

// isPeerTrusted checks if the peer IP is in the trusted proxy list.
// If no trusted proxies are configured but TrustProxyHeaders is true,
// all proxies are trusted (for backward compatibility).
func isPeerTrusted(peerIP string, cfg ProxyTrustConfig) bool {
	// If no specific proxies configured, trust all when TrustProxyHeaders is enabled
	if len(cfg.TrustedProxyCIDRs) == 0 && len(cfg.TrustedProxyIPs) == 0 {
		return true
	}

	ip := net.ParseIP(peerIP)
	if ip == nil {
		return false
	}

	// Check against trusted IPs
	if cfg.TrustedProxyIPs[ip.String()] {
		return true
	}

	// Check against trusted CIDRs
	for _, cidr := range cfg.TrustedProxyCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}
