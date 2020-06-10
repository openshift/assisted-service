package metrics

import (
	"bufio"
	"context"
	"net"
	"net/http"

	"github.com/filanov/bm-inventory/internal/metrics/matchedRouteContext"
	rmiddleware "github.com/go-openapi/runtime/middleware"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	goMiddleware "github.com/slok/go-http-metrics/middleware"
)

// Handler returns an measuring standard http.Handler. it should be added as an innerMiddleware because
// it relies on the MatchedRoute to provide more information about the route
func Handler(log logrus.FieldLogger, m goMiddleware.Middleware, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Info("Request: %s", *r)
		wi := &responseWriterInterceptor{
			statusCode:     http.StatusOK,
			ResponseWriter: w,
		}
		reporter := &myReporter{
			w:       wi,
			method:  r.Method,
			urlPath: r.URL.Path,
			ctx:     r.Context(),
		}

		mr := rmiddleware.MatchedRouteFrom(r)
		if mr != nil {
			reporter.ctx = matchedRouteContext.ToContext(reporter.ctx, mr, r.Method)
		}
		m.Measure("", reporter, func() {
			h.ServeHTTP(wi, r)
		})
	})
}

type myReporter struct {
	ctx     context.Context
	method  string
	urlPath string
	w       *responseWriterInterceptor
}

func (s *myReporter) Method() string { return s.method }

func (s *myReporter) Context() context.Context { return s.ctx }

func (s *myReporter) URLPath() string { return s.urlPath }

func (s *myReporter) StatusCode() int { return s.w.statusCode }

func (s *myReporter) BytesWritten() int64 { return int64(s.w.bytesWritten) }

// responseWriterInterceptor is a simple wrapper to intercept set data on a
// ResponseWriter.
type responseWriterInterceptor struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (w *responseWriterInterceptor) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriterInterceptor) Write(p []byte) (int, error) {
	w.bytesWritten += len(p)
	return w.ResponseWriter.Write(p)
}

func (w *responseWriterInterceptor) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("type assertion failed http.ResponseWriter not a http.Hijacker")
	}
	return h.Hijack()
}

func (w *responseWriterInterceptor) Flush() {
	f, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}

	f.Flush()
}

// Check interface implementations.
var (
	_ http.ResponseWriter   = &responseWriterInterceptor{}
	_ http.Hijacker         = &responseWriterInterceptor{}
	_ http.Flusher          = &responseWriterInterceptor{}
	_ goMiddleware.Reporter = &myReporter{}
)
