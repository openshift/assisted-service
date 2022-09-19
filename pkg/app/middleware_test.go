package app

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		logger.SetOutput(io.Discard)
	})

	It("monitors threads at /health", func() {
		timeout := 20 * time.Millisecond
		mHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		failure := thread.New(logger, "failed test", 10*time.Millisecond, func() {
		})
		failure.Start()
		req := httptest.NewRequest("GET", "/health", nil)
		h1 := WithHealthMiddleware(mHandler, []*thread.Thread{failure}, logger, timeout)
		h1 = WithIPXEScriptMiddleware(h1)

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
		h1 := WithHealthMiddleware(baseHandler, nil, logger, time.Minute)
		h1 = WithIPXEScriptMiddleware(h1)
		server := httptest.NewServer(h1)
		client := server.Client()
		defer server.Close()

		resp, err := client.Get(server.URL + "/ready")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})

var _ = Describe("WithInitrdViaHTTPMiddleware", func() {
	var (
		httpServer  *httptest.Server
		httpsServer *httptest.Server
		httpClient  *http.Client
		httpsClient *http.Client
	)

	BeforeEach(func() {
		mux := http.NewServeMux()
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "Hello!")
		}))
		httpServer = httptest.NewServer(WithIPXEScriptMiddleware(mux))
		httpsServer = httptest.NewTLSServer(WithIPXEScriptMiddleware(mux))
		httpClient = httpServer.Client()
		httpsClient = httpsServer.Client()
	})

	AfterEach(func() {
		httpServer.Close()
		httpsServer.Close()
	})

	doRequestWithPath := func(path string, queryString map[string]string, useHttps bool) int {
		var server *httptest.Server
		var client *http.Client
		if useHttps {
			server = httpsServer
			client = httpsClient
		} else {
			server = httpServer
			client = httpClient
		}

		requestUrl, err := url.Parse(server.URL)
		Expect(err).NotTo(HaveOccurred())
		requestUrl.Path = path
		q := requestUrl.Query()
		for k, v := range queryString {
			q.Add(k, v)
		}
		requestUrl.RawQuery = q.Encode()

		req, err := http.NewRequest(http.MethodGet, requestUrl.String(), nil)
		Expect(err).NotTo(HaveOccurred())
		resp, err := client.Do(req)
		Expect(err).NotTo(HaveOccurred())

		return resp.StatusCode
	}

	It("filters http requests", func() {
		respStatus := doRequestWithPath("/api/assisted-install/v2/infra-envs/a7acfb01-d89f-40c8-82d7-02b20cf00173/downloads/files", map[string]string{"file_name": "ipxe-script"}, false)
		Expect(respStatus).To(Equal(200))

		respStatus = doRequestWithPath("/api/assisted-install/v2/infra-envs/a7acfb01-d89f-40c8-82d7-02b20cf00173/downloads/files", map[string]string{"file_name": "some-other-file"}, false)
		Expect(respStatus).To(Equal(404))

		respStatus = doRequestWithPath("/api/assisted-install/v2/infra-envs/a7acfb01-d89f-40c8-82d7-02b20cf00173/downloads/files", map[string]string{"i_cant_believe_its_not_filename": "ipxe-script"}, false)
		Expect(respStatus).To(Equal(404))

		respStatus = doRequestWithPath("/api/assisted-install/v2/infra-envs/a7acfb01-d89f-40c8-82d7-02b20cf00173/secret", map[string]string{}, false)
		Expect(respStatus).To(Equal(404))

		respStatus = doRequestWithPath("/api/assisted-install/v2/infra-envs/foobar/downloads/files", map[string]string{"file_name": "ipxe-script"}, false)
		Expect(respStatus).To(Equal(404))

		respStatus = doRequestWithPath("/health", map[string]string{}, false)
		Expect(respStatus).To(Equal(200))

		respStatus = doRequestWithPath("/api/assisted-install/v2/infra-envs/a7acfb01-d89f-40c8-82d7-02b20cf00173/downloads/files", map[string]string{"file_name": "ipxe-script"}, true)
		Expect(respStatus).To(Equal(200))

		respStatus = doRequestWithPath("/api/assisted-install/v2/infra-envs/foobar/downloads/files", map[string]string{"file_name": "ipxe-script"}, true)
		Expect(respStatus).To(Equal(200))

		respStatus = doRequestWithPath("/health", map[string]string{}, true)
		Expect(respStatus).To(Equal(200))
	})
})
