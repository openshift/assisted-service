package app

import (
	"net/http"
	"net/http/httptest"
	"regexp"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("WithPathAllowListMiddleware", func() {
	var successHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	It("fails for non-matching methods", func() {
		req, err := http.NewRequest(http.MethodPatch, "/testing", nil)
		Expect(err).NotTo(HaveOccurred())

		rr := httptest.NewRecorder()

		allowList := []*RequestMatch{
			{
				Path:    regexp.MustCompile(`/testing$`),
				Methods: []string{http.MethodGet, http.MethodHead},
			},
		}

		WithPathAllowListMiddleware(successHandler, allowList).ServeHTTP(rr, req)
		Expect(rr.Code).To(Equal(http.StatusForbidden))
	})

	It("fails for non-matching paths", func() {
		req, err := http.NewRequest(http.MethodPatch, "/testing", nil)
		Expect(err).NotTo(HaveOccurred())

		rr := httptest.NewRecorder()

		allowList := []*RequestMatch{
			{
				Path:    regexp.MustCompile(`/update$`),
				Methods: []string{http.MethodPatch},
			},
		}

		WithPathAllowListMiddleware(successHandler, allowList).ServeHTTP(rr, req)
		Expect(rr.Code).To(Equal(http.StatusForbidden))
	})

	It("succeeds with matching requests", func() {
		req, err := http.NewRequest(http.MethodPatch, "/testing", nil)
		Expect(err).NotTo(HaveOccurred())

		rr := httptest.NewRecorder()

		allowList := []*RequestMatch{
			{
				Path:    regexp.MustCompile(`/testing`),
				Methods: []string{http.MethodGet, http.MethodPatch},
			},
		}

		WithPathAllowListMiddleware(successHandler, allowList).ServeHTTP(rr, req)
		Expect(rr.Code).To(Equal(http.StatusOK))
	})

	It("succeeds when the request matches only one list entry", func() {
		req, err := http.NewRequest(http.MethodPatch, "/testing", nil)
		Expect(err).NotTo(HaveOccurred())

		rr := httptest.NewRecorder()

		allowList := []*RequestMatch{
			{
				Path:    regexp.MustCompile(`/health`),
				Methods: []string{http.MethodGet},
			},
			{
				Path:    regexp.MustCompile(`/testing$`),
				Methods: []string{http.MethodPatch},
			},
			{
				Path:    regexp.MustCompile(`/clusters/.*$`),
				Methods: []string{http.MethodGet},
			},
		}

		WithPathAllowListMiddleware(successHandler, allowList).ServeHTTP(rr, req)
		Expect(rr.Code).To(Equal(http.StatusOK))
	})

	It("succeeds with more complicated regex", func() {
		req, err := http.NewRequest(http.MethodGet, "/clusters/f4db0f5b-a93e-434b-a270-43665cfbae63/downloads/image", nil)
		Expect(err).NotTo(HaveOccurred())

		rr := httptest.NewRecorder()

		allowList := []*RequestMatch{
			{
				Path:    regexp.MustCompile(`/clusters/[\w-]+/downloads/image$`),
				Methods: []string{http.MethodGet, http.MethodHead},
			},
		}

		WithPathAllowListMiddleware(successHandler, allowList).ServeHTTP(rr, req)
		Expect(rr.Code).To(Equal(http.StatusOK))
	})
})
