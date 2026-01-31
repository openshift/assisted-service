package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestRateLimit(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RateLimit Suite")
}

var _ = Describe("RateLimiter", func() {
	var (
		limiter *RateLimiter
	)

	AfterEach(func() {
		if limiter != nil {
			limiter.Stop()
		}
	})

	Describe("NewRateLimiter", func() {
		It("should create a limiter with specified rate and burst", func() {
			limiter = NewRateLimiter(10.0, 20, time.Hour)
			Expect(limiter.Rate()).To(BeNumerically("==", 10.0))
			Expect(limiter.Burst()).To(Equal(20))
		})
	})

	Describe("Allow", func() {
		It("should allow requests within the rate limit", func() {
			// 10 RPS with burst of 5
			limiter = NewRateLimiter(10.0, 5, time.Hour)

			// Should allow burst number of requests immediately
			for i := 0; i < 5; i++ {
				Expect(limiter.Allow("client1")).To(BeTrue())
			}
		})

		It("should deny requests exceeding the rate limit", func() {
			// 1 RPS with burst of 2
			limiter = NewRateLimiter(1.0, 2, time.Hour)

			// Allow burst
			Expect(limiter.Allow("client1")).To(BeTrue())
			Expect(limiter.Allow("client1")).To(BeTrue())

			// Should be denied now (burst exhausted)
			Expect(limiter.Allow("client1")).To(BeFalse())
		})

		It("should maintain separate limits per client", func() {
			limiter = NewRateLimiter(1.0, 2, time.Hour)

			// Exhaust client1's burst
			Expect(limiter.Allow("client1")).To(BeTrue())
			Expect(limiter.Allow("client1")).To(BeTrue())
			Expect(limiter.Allow("client1")).To(BeFalse())

			// client2 should still have full burst
			Expect(limiter.Allow("client2")).To(BeTrue())
			Expect(limiter.Allow("client2")).To(BeTrue())
		})
	})

	Describe("GetLimiter", func() {
		It("should return the same limiter for the same client", func() {
			limiter = NewRateLimiter(10.0, 5, time.Hour)

			limiter1 := limiter.GetLimiter("client1")
			limiter2 := limiter.GetLimiter("client1")

			Expect(limiter1).To(BeIdenticalTo(limiter2))
		})

		It("should return different limiters for different clients", func() {
			limiter = NewRateLimiter(10.0, 5, time.Hour)

			limiter1 := limiter.GetLimiter("client1")
			limiter2 := limiter.GetLimiter("client2")

			Expect(limiter1).NotTo(BeIdenticalTo(limiter2))
		})
	})

	Describe("Tokens", func() {
		It("should return available tokens", func() {
			limiter = NewRateLimiter(10.0, 5, time.Hour)

			// Initial tokens should be burst size
			tokens := limiter.Tokens("client1")
			Expect(tokens).To(BeNumerically("~", 5.0, 0.1))

			// Use one token
			limiter.Allow("client1")
			tokens = limiter.Tokens("client1")
			Expect(tokens).To(BeNumerically("~", 4.0, 0.1))
		})
	})

	Describe("cleanup", func() {
		It("should remove stale entries", func() {
			limiter = NewRateLimiter(10.0, 5, 10*time.Millisecond)

			// Create a visitor
			limiter.GetLimiter("client1")

			// Check initial count with proper locking
			limiter.mu.RLock()
			initialCount := len(limiter.visitors)
			limiter.mu.RUnlock()
			Expect(initialCount).To(Equal(1))

			// Wait for cleanup (3x cleanup frequency = 30ms, plus some buffer)
			time.Sleep(50 * time.Millisecond)

			limiter.mu.RLock()
			count := len(limiter.visitors)
			limiter.mu.RUnlock()

			Expect(count).To(Equal(0))
		})

		It("should not panic with zero cleanup interval", func() {
			// This should not panic - cleanup loop should just return early
			limiter = NewRateLimiter(10.0, 5, 0)
			Expect(limiter).NotTo(BeNil())

			// Create a visitor to verify the limiter works normally
			Expect(limiter.Allow("client1")).To(BeTrue())
		})

		It("should not panic with negative cleanup interval", func() {
			// This should not panic - cleanup loop should just return early
			limiter = NewRateLimiter(10.0, 5, -time.Minute)
			Expect(limiter).NotTo(BeNil())

			// Create a visitor to verify the limiter works normally
			Expect(limiter.Allow("client1")).To(BeTrue())
		})
	})

	Describe("Stop", func() {
		It("should not panic when called multiple times", func() {
			limiter = NewRateLimiter(10.0, 5, time.Hour)

			// Calling Stop multiple times should not panic
			Expect(func() {
				limiter.Stop()
				limiter.Stop()
				limiter.Stop()
			}).NotTo(Panic())

			// Set limiter to nil so AfterEach doesn't call Stop again
			limiter = nil
		})
	})
})

var _ = Describe("GetClientID", func() {
	It("should extract IP from RemoteAddr when proxy headers not trusted", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"

		// GetClientID defaults to not trusting proxy headers
		clientID := GetClientID(req)
		Expect(clientID).To(Equal("ip:192.168.1.100"))
	})

	It("should ignore X-Forwarded-For header when proxy headers not trusted", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")

		// GetClientID defaults to not trusting proxy headers for security
		clientID := GetClientID(req)
		Expect(clientID).To(Equal("ip:192.168.1.100"))
	})

	It("should ignore X-Real-IP header when proxy headers not trusted", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("X-Real-IP", "10.0.0.5")

		// GetClientID defaults to not trusting proxy headers for security
		clientID := GetClientID(req)
		Expect(clientID).To(Equal("ip:192.168.1.100"))
	})
})

var _ = Describe("GetClientIDWithConfig", func() {
	var trustedConfig ProxyTrustConfig

	BeforeEach(func() {
		trustedConfig = ProxyTrustConfig{TrustProxyHeaders: true}
	})

	It("should use authenticated user ID when present in context", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"

		// Set authenticated user in context
		ctx := SetAuthenticatedUser(req.Context(), "user-123")
		req = req.WithContext(ctx)

		clientID := GetClientIDWithConfig(req, trustedConfig)
		Expect(clientID).To(Equal("user:user-123"))
	})

	It("should fall back to IP when no authenticated user in context", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"

		clientID := GetClientIDWithConfig(req, trustedConfig)
		Expect(clientID).To(Equal("ip:192.168.1.100"))
	})

	It("should extract IP from RemoteAddr", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"

		clientID := GetClientIDWithConfig(req, trustedConfig)
		Expect(clientID).To(Equal("ip:192.168.1.100"))
	})

	It("should prefer X-Forwarded-For header when proxy is trusted", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")

		clientID := GetClientIDWithConfig(req, trustedConfig)
		Expect(clientID).To(Equal("ip:10.0.0.1"))
	})

	It("should use X-Real-IP when X-Forwarded-For is not present and proxy is trusted", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("X-Real-IP", "10.0.0.5")

		clientID := GetClientIDWithConfig(req, trustedConfig)
		Expect(clientID).To(Equal("ip:10.0.0.5"))
	})

	It("should not trust headers when peer is not in trusted proxy list", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("X-Forwarded-For", "10.0.0.1")

		// Configure to only trust specific proxies
		cfg := ParseTrustedProxies(true, "10.0.0.0/8")
		clientID := GetClientIDWithConfig(req, cfg)
		// 192.168.1.100 is not in 10.0.0.0/8, so headers should not be trusted
		Expect(clientID).To(Equal("ip:192.168.1.100"))
	})

	It("should trust headers when peer is in trusted proxy list", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.50:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.1")

		// Configure to trust 10.0.0.0/8 proxies
		cfg := ParseTrustedProxies(true, "10.0.0.0/8")
		clientID := GetClientIDWithConfig(req, cfg)
		// 10.0.0.50 is in 10.0.0.0/8, so headers should be trusted
		Expect(clientID).To(Equal("ip:203.0.113.1"))
	})
})
