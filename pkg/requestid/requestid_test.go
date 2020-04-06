package requestid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/mock"
)

type mockTransport struct {
	mock.Mock
}

func (m *mockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	m.Called(r)
	return nil, nil
}

func TestTransport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		prepare func(t *testing.T, tr *mockTransport) *http.Request
	}{
		{
			name: "happy flow",
			prepare: func(t *testing.T, tr *mockTransport) *http.Request {
				const requestID = "1234"

				match := mock.MatchedBy(func(req *http.Request) bool {
					return req.Header.Get(headerKey) == requestID
				})

				tr.On("RoundTrip", match).Return(nil, nil).Once()

				ctx := context.WithValue(context.Background(), ctxKey, requestID)
				req := httptest.NewRequest(http.MethodGet, "http://example.org", nil)
				req = req.WithContext(ctx)
				return req
			},
		},
		{
			name: "no request id in context",
			prepare: func(t *testing.T, tr *mockTransport) *http.Request {
				match := mock.MatchedBy(func(req *http.Request) bool {
					return req.Header.Get(headerKey) == ""
				})

				tr.On("RoundTrip", match).Return(nil, nil).Once()

				req := httptest.NewRequest(http.MethodGet, "http://example.org", nil)
				return req
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tr mockTransport
			defer tr.AssertExpectations(t)
			req := tt.prepare(t, &tr)
			_, _ = Transport(&tr).RoundTrip(req)
		})
	}
}
