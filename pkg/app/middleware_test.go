package app

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/pkg/thread"
	"github.com/sirupsen/logrus"
)

func TestMiddleWare(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Middleware test")
}

var _ = Describe("WithHealthMiddleware", func() {
	var (
		logger *logrus.Logger
	)

	BeforeEach(func() {
		logger = logrus.New()
		logger.SetOutput(ioutil.Discard)
	})

	It("monitors threads at /health", func() {
		timeout := 20 * time.Millisecond
		mHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		failure := thread.New(logger, "failed test", 10*time.Millisecond, func() {
		})
		failure.Start()
		req := httptest.NewRequest("GET", "/health", nil)
		h1 := WithHealthMiddleware(mHandler, []*thread.Thread{failure}, logger, timeout)

		By("Testing healthycheck success when thread is running")
		successCounter := 0
		Eventually(func() bool {
			rr := httptest.NewRecorder()
			h1.ServeHTTP(rr, req)
			if rr.Code == http.StatusOK {
				successCounter += 1
			}
			return successCounter == 3
		}, 1*time.Second, 10*time.Millisecond).Should(BeTrue())

		By("Verifying healthcheck failed when thread stopped")

		failure.Stop()
		// wait more than monitored threshold
		Eventually(func() bool {
			rr := httptest.NewRecorder()
			h1.ServeHTTP(rr, req)
			return rr.Code == http.StatusInternalServerError
		}, 1*time.Second, 10*time.Millisecond).Should(BeTrue())
	})

	It("returns OK at /ready", func() {
		baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "Hello!")
		})
		server := httptest.NewServer(WithHealthMiddleware(baseHandler, nil, logger, time.Minute))
		client := server.Client()
		defer server.Close()

		resp, err := client.Get(server.URL + "/ready")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})

var _ = Describe("DisableV1Middleware", func() {
	var (
		server *httptest.Server
		client *http.Client
	)

	BeforeEach(func() {
		baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "Hello!")
		})

		server = httptest.NewServer(DisableV1Middleware(baseHandler))
		client = server.Client()
	})

	AfterEach(func() {
		server.Close()
	})

	expectStatusCode := func(path string, code int) {
		resp, err := client.Get(server.URL + path)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(code))
	}

	It("calls base handler for non-v1 paths", func() {
		expectStatusCode("/api/assisted-install/v2/clusters", http.StatusOK)
		expectStatusCode("/api/assisted-install/v3/clusters", http.StatusOK)
		expectStatusCode("/api/assisted-install/whatever", http.StatusOK)
	})

	It("returns 404 for v1 paths", func() {
		expectStatusCode("/api/assisted-install/v1/clusters", http.StatusNotFound)
		expectStatusCode("/api/assisted-install/v1/events", http.StatusNotFound)
		expectStatusCode("/api/assisted-install/v1/whatever", http.StatusNotFound)
	})
})
