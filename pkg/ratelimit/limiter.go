package ratelimit

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

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
}

// visitorState tracks rate limiting state for a single client
type visitorState struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter creates a new rate limiter with the specified requests per second
// and burst size. The cleanupFreq parameter controls how often stale entries are
// removed from the visitor map.
func NewRateLimiter(rps float64, burst int, cleanupFreq time.Duration) *RateLimiter {
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

// Stop stops the cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopCleanup)
}

// cleanupLoop periodically removes stale visitor entries that haven't been seen
// for a configurable duration.
func (rl *RateLimiter) cleanupLoop() {
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
func GetClientID(r *http.Request) string {
	return "ip:" + getClientIP(r)
}

// getClientIP extracts the client IP address from the request,
// considering X-Forwarded-For and X-Real-IP headers for proxied requests.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (may contain multiple IPs)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP (original client)
		if idx := len(xff); idx > 0 {
			for i, c := range xff {
				if c == ',' {
					return xff[:i]
				}
			}
			return xff
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
