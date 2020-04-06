package log

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/filanov/bm-inventory/pkg/requestid"
	"github.com/filanov/bm-inventory/pkg/testutil"
	"github.com/stretchr/testify/assert"
)

func TestFromContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		request func() *http.Request
		assert  func(t *testing.T, logOutput string)
	}{
		{
			name: "happy flow",
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "http://example.org", nil)
				req.Header.Set("X-Request-ID", "1234")
				return req
			},
			assert: func(t *testing.T, logOutput string) {
				assert.Contains(t, logOutput, "request_id=1234")
			},
		},
		{
			name: "no request-id header",
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.org", nil)
			},
			assert: func(t *testing.T, logOutput string) {
				assert.Regexp(t, `request_id=[[:alnum:]]+`, logOutput)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			logOut := bytes.NewBuffer(nil)
			logger := testutil.Log()
			logger.Out = logOut

			h := requestid.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				log := FromContext(r.Context(), logger)
				log.Warnf("test")
			}))

			req := tt.request()
			h.ServeHTTP(httptest.NewRecorder(), req)
			logOutput := logOut.String()
			t.Logf("Got output %s", logOutput)

			tt.assert(t, logOutput)
		})
	}
}
