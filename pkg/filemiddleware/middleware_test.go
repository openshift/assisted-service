package filemiddleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-openapi/runtime"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestFileMiddleware(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "filemiddleware")
}

type nopResponder struct{}

func (m nopResponder) WriteResponse(_ http.ResponseWriter, _ runtime.Producer) {}

var _ = Describe("WriteResponse", func() {
	var mockProducer runtime.Producer
	nopProduce := func(_ io.Writer, _ interface{}) error { return nil }

	BeforeEach(func() {
		mockProducer = runtime.ProducerFunc(nopProduce)
	})

	It("sets Content-Disposition correctly", func() {
		rec := httptest.NewRecorder()
		NewResponder(nopResponder{}, "myfile.name", 0, nil).WriteResponse(rec, mockProducer)
		resp := rec.Result()

		Expect(resp.Header.Get("Content-Disposition")).To(Equal("attachment; filename=\"myfile.name\""))
	})

	It("sets Content-Length when provided", func() {
		rec := httptest.NewRecorder()
		NewResponder(nopResponder{}, "myfile.name", 12345, nil).WriteResponse(rec, mockProducer)
		resp := rec.Result()

		Expect(resp.Header.Get("Content-Length")).To(Equal("12345"))
	})

	It("does not set Content-Length when 0 is provided", func() {
		rec := httptest.NewRecorder()
		NewResponder(nopResponder{}, "myfile.name", 0, nil).WriteResponse(rec, mockProducer)
		resp := rec.Result()

		_, present := resp.Header["Content-Length"]
		Expect(present).To(BeFalse())
	})

	It("sets Last-Modified when provided", func() {
		rec := httptest.NewRecorder()
		t, err := http.ParseTime("Fri, 22 Apr 2022 18:11:09 GMT")
		Expect(err).NotTo(HaveOccurred())
		NewResponder(nopResponder{}, "myfile.name", 0, &t).WriteResponse(rec, mockProducer)
		resp := rec.Result()

		Expect(resp.Header.Get("Last-Modified")).To(Equal("Fri, 22 Apr 2022 18:11:09 GMT"))
	})

	It("does not set Last-Modified when a zero-value time is provided", func() {
		rec := httptest.NewRecorder()
		NewResponder(nopResponder{}, "myfile.name", 0, nil).WriteResponse(rec, mockProducer)
		resp := rec.Result()

		_, present := resp.Header["Last-Modified"]
		Expect(present).To(BeFalse())
	})
})
