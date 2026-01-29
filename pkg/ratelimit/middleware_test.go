package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Middleware", func() {
	var (
		middleware *Middleware
		handler    http.Handler
		recorder   *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		recorder = httptest.NewRecorder()
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		})
	})

	AfterEach(func() {
		if middleware != nil && middleware.limiters != nil {
			StopLimiters(middleware.limiters)
		}
	})

	Describe("Handler", func() {
		Context("when rate limiting is disabled", func() {
			BeforeEach(func() {
				middleware = NewMiddleware(MiddlewareConfig{
					Enabled: false,
					Log:     logrus.New(),
				})
			})

			It("should pass through all requests", func() {
				req := httptest.NewRequest(http.MethodGet, "/v2/clusters", nil)
				req.RemoteAddr = "192.168.1.1:1234"

				for i := 0; i < 100; i++ {
					recorder = httptest.NewRecorder()
					middleware.Handler(handler).ServeHTTP(recorder, req)
					Expect(recorder.Code).To(Equal(http.StatusOK))
				}
			})
		})

		Context("when rate limiting is enabled", func() {
			BeforeEach(func() {
				limiters := map[EndpointCategory]*RateLimiter{
					CategoryDefault: NewRateLimiter(1.0, 2, time.Hour),
					CategoryRead:    NewRateLimiter(1.0, 2, time.Hour),
					CategoryWrite:   NewRateLimiter(0.5, 1, time.Hour),
				}
				middleware = NewMiddleware(MiddlewareConfig{
					Enabled:  true,
					Limiters: limiters,
					Log:      logrus.New(),
				})
			})

			It("should allow requests within limit", func() {
				req := httptest.NewRequest(http.MethodGet, "/v2/clusters", nil)
				req.RemoteAddr = "192.168.1.1:1234"

				middleware.Handler(handler).ServeHTTP(recorder, req)
				Expect(recorder.Code).To(Equal(http.StatusOK))
			})

			It("should return 429 when rate limit exceeded", func() {
				req := httptest.NewRequest(http.MethodGet, "/v2/clusters", nil)
				req.RemoteAddr = "192.168.1.1:1234"

				// Exhaust burst
				for i := 0; i < 2; i++ {
					recorder = httptest.NewRecorder()
					middleware.Handler(handler).ServeHTTP(recorder, req)
					Expect(recorder.Code).To(Equal(http.StatusOK))
				}

				// Next request should be rate limited
				recorder = httptest.NewRecorder()
				middleware.Handler(handler).ServeHTTP(recorder, req)
				Expect(recorder.Code).To(Equal(http.StatusTooManyRequests))
			})

			It("should set rate limit headers with requests per minute", func() {
				req := httptest.NewRequest(http.MethodGet, "/v2/clusters", nil)
				req.RemoteAddr = "192.168.1.1:1234"

				middleware.Handler(handler).ServeHTTP(recorder, req)

				// CategoryRead has 1.0 RPS = 60 requests/minute
				Expect(recorder.Header().Get("X-RateLimit-Limit")).To(Equal("60"))
				// Headers are set before Allow() consumes a token.
				// With burst of 2 and fresh limiter, Tokens() returns 2.0, so 2 * 60 = 120 req/min.
				Expect(recorder.Header().Get("X-RateLimit-Remaining")).To(Equal("120"))
			})

			It("should set Retry-After header when rate limited", func() {
				req := httptest.NewRequest(http.MethodGet, "/v2/clusters", nil)
				req.RemoteAddr = "192.168.1.1:1234"

				// Exhaust burst
				for i := 0; i < 3; i++ {
					recorder = httptest.NewRecorder()
					middleware.Handler(handler).ServeHTTP(recorder, req)
				}

				Expect(recorder.Header().Get("Retry-After")).To(Equal("60"))
			})

			It("should maintain separate limits per client", func() {
				req1 := httptest.NewRequest(http.MethodGet, "/v2/clusters", nil)
				req1.RemoteAddr = "192.168.1.1:1234"

				req2 := httptest.NewRequest(http.MethodGet, "/v2/clusters", nil)
				req2.RemoteAddr = "192.168.1.2:1234"

				// Exhaust client1's burst
				for i := 0; i < 3; i++ {
					recorder = httptest.NewRecorder()
					middleware.Handler(handler).ServeHTTP(recorder, req1)
				}
				Expect(recorder.Code).To(Equal(http.StatusTooManyRequests))

				// client2 should still be allowed
				recorder = httptest.NewRecorder()
				middleware.Handler(handler).ServeHTTP(recorder, req2)
				Expect(recorder.Code).To(Equal(http.StatusOK))
			})

			It("should convert sub-1 RPS to meaningful requests per minute", func() {
				// Create middleware with sub-1 RPS limiter (0.5 RPS = 30 req/min)
				subRPSLimiters := map[EndpointCategory]*RateLimiter{
					CategoryDefault: NewRateLimiter(0.5, 5, time.Hour),
					CategoryRead:    NewRateLimiter(0.1, 5, time.Hour), // 0.1 RPS = 6 req/min
				}
				subRPSMiddleware := NewMiddleware(MiddlewareConfig{
					Enabled:  true,
					Limiters: subRPSLimiters,
					Log:      logrus.New(),
				})
				defer StopLimiters(subRPSMiddleware.limiters)

				req := httptest.NewRequest(http.MethodGet, "/v2/clusters", nil)
				req.RemoteAddr = "192.168.1.100:1234"
				recorder = httptest.NewRecorder()

				subRPSMiddleware.Handler(handler).ServeHTTP(recorder, req)

				// 0.1 RPS * 60 = 6 requests per minute
				Expect(recorder.Header().Get("X-RateLimit-Limit")).To(Equal("6"))
			})
		})

		Context("when skipping endpoints", func() {
			BeforeEach(func() {
				limiters := map[EndpointCategory]*RateLimiter{
					CategoryDefault: NewRateLimiter(0.01, 1, time.Hour), // Very restrictive
				}
				middleware = NewMiddleware(MiddlewareConfig{
					Enabled:  true,
					Limiters: limiters,
					Log:      logrus.New(),
				})
			})

			It("should not rate limit /health", func() {
				req := httptest.NewRequest(http.MethodGet, "/health", nil)
				req.RemoteAddr = "192.168.1.1:1234"

				for i := 0; i < 10; i++ {
					recorder = httptest.NewRecorder()
					middleware.Handler(handler).ServeHTTP(recorder, req)
					Expect(recorder.Code).To(Equal(http.StatusOK))
				}
			})

			It("should not rate limit /ready", func() {
				req := httptest.NewRequest(http.MethodGet, "/ready", nil)
				req.RemoteAddr = "192.168.1.1:1234"

				for i := 0; i < 10; i++ {
					recorder = httptest.NewRecorder()
					middleware.Handler(handler).ServeHTTP(recorder, req)
					Expect(recorder.Code).To(Equal(http.StatusOK))
				}
			})

			It("should not rate limit /metrics", func() {
				req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
				req.RemoteAddr = "192.168.1.1:1234"

				for i := 0; i < 10; i++ {
					recorder = httptest.NewRecorder()
					middleware.Handler(handler).ServeHTTP(recorder, req)
					Expect(recorder.Code).To(Equal(http.StatusOK))
				}
			})
		})
	})

	Describe("categorizeEndpoint", func() {
		BeforeEach(func() {
			middleware = NewMiddleware(MiddlewareConfig{
				Enabled: true,
				Log:     logrus.New(),
			})
		})

		It("should categorize download endpoints correctly", func() {
			paths := []string{
				"/v2/clusters/123/downloads/files",
				"/v2/infra-envs/123/downloads/image",
				"/api/assisted-install/v2/clusters/123/downloads/credentials",
			}

			for _, path := range paths {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				category := middleware.categorizeEndpoint(req)
				Expect(category).To(Equal(CategoryDownload), "path: %s", path)
			}
		})

		It("should categorize GET requests as read", func() {
			req := httptest.NewRequest(http.MethodGet, "/v2/clusters", nil)
			category := middleware.categorizeEndpoint(req)
			Expect(category).To(Equal(CategoryRead))
		})

		It("should categorize POST requests as write", func() {
			req := httptest.NewRequest(http.MethodPost, "/v2/clusters", nil)
			category := middleware.categorizeEndpoint(req)
			Expect(category).To(Equal(CategoryWrite))
		})

		It("should categorize DELETE requests as write", func() {
			req := httptest.NewRequest(http.MethodDelete, "/v2/clusters/123", nil)
			category := middleware.categorizeEndpoint(req)
			Expect(category).To(Equal(CategoryWrite))
		})
	})
})

var _ = Describe("Config", func() {
	Describe("CreateLimiters", func() {
		It("should create limiters for all categories", func() {
			config := Config{
				Enabled:         true,
				AuthRPS:         0.1,
				AuthBurst:       5,
				ReadRPS:         1.67,
				ReadBurst:       100,
				WriteRPS:        0.33,
				WriteBurst:      20,
				DownloadRPS:     0.17,
				DownloadBurst:   10,
				DefaultRPS:      1.67,
				DefaultBurst:    100,
				CleanupInterval: time.Hour,
			}

			limiters := config.CreateLimiters()
			defer StopLimiters(limiters)

			Expect(limiters).To(HaveKey(CategoryAuth))
			Expect(limiters).To(HaveKey(CategoryRead))
			Expect(limiters).To(HaveKey(CategoryWrite))
			Expect(limiters).To(HaveKey(CategoryDownload))
			Expect(limiters).To(HaveKey(CategoryDefault))

			Expect(limiters[CategoryAuth].Burst()).To(Equal(5))
			Expect(limiters[CategoryRead].Burst()).To(Equal(100))
			Expect(limiters[CategoryWrite].Burst()).To(Equal(20))
			Expect(limiters[CategoryDownload].Burst()).To(Equal(10))
		})
	})

	Describe("CreateMiddleware", func() {
		It("should create a configured middleware", func() {
			config := Config{
				Enabled:         true,
				DefaultRPS:      1.0,
				DefaultBurst:    10,
				CleanupInterval: time.Hour,
			}

			middleware := config.CreateMiddleware(logrus.New())
			defer StopLimiters(middleware.limiters)

			Expect(middleware).NotTo(BeNil())
			Expect(middleware.enabled).To(BeTrue())
			Expect(middleware.limiters).NotTo(BeNil())
		})

		It("should not create limiters when disabled", func() {
			config := Config{
				Enabled:         false,
				DefaultRPS:      1.0,
				DefaultBurst:    10,
				CleanupInterval: time.Hour,
			}

			middleware := config.CreateMiddleware(logrus.New())

			Expect(middleware).NotTo(BeNil())
			Expect(middleware.enabled).To(BeFalse())
			Expect(middleware.limiters).To(BeNil())
		})
	})
})
