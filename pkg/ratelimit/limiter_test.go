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
			Expect(limiter.visitors).To(HaveLen(1))

			// Wait for cleanup (3x cleanup frequency = 30ms, plus some buffer)
			time.Sleep(50 * time.Millisecond)

			limiter.mu.RLock()
			count := len(limiter.visitors)
			limiter.mu.RUnlock()

			Expect(count).To(Equal(0))
		})
	})
})

var _ = Describe("GetClientID", func() {
	It("should extract IP from RemoteAddr", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"

		clientID := GetClientID(req)
		Expect(clientID).To(Equal("ip:192.168.1.100"))
	})

	It("should prefer X-Forwarded-For header", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")

		clientID := GetClientID(req)
		Expect(clientID).To(Equal("ip:10.0.0.1"))
	})

	It("should use X-Real-IP when X-Forwarded-For is not present", func() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("X-Real-IP", "10.0.0.5")

		clientID := GetClientID(req)
		Expect(clientID).To(Equal("ip:10.0.0.5"))
	})
})
