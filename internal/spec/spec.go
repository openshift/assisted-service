package spec

import (
	"net/http"
	"path"

	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/restapi"
)

var openapiPath = path.Join(client.DefaultBasePath, "openapi")

// WithSpecMiddleware returns middleware which responds to the openapi endpoint
func WithSpecMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == openapiPath {
			_, _ = w.Write(restapi.SwaggerJSON)
			return
		}
		next.ServeHTTP(w, r)
	})
}
