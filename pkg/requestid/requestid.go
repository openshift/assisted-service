package requestid

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type requestIDKey string

// Allow getting requestID from environment variable. To be used in tools, not in services
type Config struct {
	RequestID string `envconfig:"X-REQUEST-ID" default:""`
}

const (
	headerKey              = "X-Request-ID"
	ctxKey    requestIDKey = "request-id"
)

// Middleware wraps an http handler.
// Before the inner handler is called, the X-Request-ID header is extracted
// and injected into the request context.
// It is safe to be called if the header does not exists
func Middleware(inner http.Handler) http.Handler {
	return handler{inner: inner}
}

// Transport wraps an inner transport.
// It adds the request-id from the request context to the request header.
func Transport(inner http.RoundTripper) http.RoundTripper {
	return transport{inner: inner}
}

// ApplyTransport injects the request-id transport to an http client
func ApplyTransport(client *http.Client) {
	inner := client.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	client.Transport = Transport(inner)
}

// FromContext returns the request id stored in the the context
func FromContext(ctx context.Context) string {
	requestID := ctx.Value(ctxKey)
	if requestID == nil {
		requestID = ""
	}
	return requestID.(string)
}

func ToContext(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, ctxKey, requestID)
}

func RequestIDLogger(logger logrus.FieldLogger, requestID string) logrus.FieldLogger {
	return logger.WithField("request_id", requestID)
}

type handler struct {
	inner http.Handler
}

func FromRequest(r *http.Request) string {
	return r.Header.Get(headerKey)
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := FromRequest(r)
	if requestID == "" {
		requestID = NewID()
	}
	ctx := ToContext(r.Context(), requestID)
	r = r.WithContext(ctx)
	h.inner.ServeHTTP(w, r)
}

type transport struct {
	inner http.RoundTripper
}

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if requestID := r.Context().Value(ctxKey); requestID != nil {
		r.Header.Set(headerKey, requestID.(string))
	}
	return t.inner.RoundTrip(r)
}

func NewID() string {
	return uuid.New().String()
}
