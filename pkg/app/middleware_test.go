package app

import (
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

var _ = Describe("Middleware test", func() {

	l := logrus.New()
	l.SetOutput(ioutil.Discard)

	It("Healthcheck test", func() {
		timeout := 20 * time.Millisecond
		mHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		failure := thread.New(l, "failed test", 10*time.Millisecond, func() {
		})
		failure.Start()
		req := httptest.NewRequest("GET", "/health", nil)
		h1 := WithHealthMiddleware(mHandler, []*thread.Thread{failure}, l, timeout)

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
})
