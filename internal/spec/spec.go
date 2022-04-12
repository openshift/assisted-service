package spec

import (
	"net/http"
	"path"

	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/restapi"
	"github.com/thoas/go-funk"
)

var openapiPaths []string

func init() {
	openapiPaths = append(openapiPaths,
		// We serve our API spec on <base_path>/openapi - the API spec contains the
		// definitions for all currently supported versions of our API
		path.Join(client.DefaultBasePath, "openapi"),

		// We also serve the exact same API spec (containing the definitions for all
		// versions) on <base_path>/<version>/openapi because API UIs such as the one at
		// https://api.openshift.com/ always try to access this endpoint but with some
		// version in the path
		path.Join(client.DefaultBasePath, "v2", "openapi"),

		// Add future versions here, e.g.:
		// path.Join(client.DefaultBasePath, "v3", "openapi"),
	)
}

// WithSpecMiddleware returns middleware which responds to the openapi endpoint
func WithSpecMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && funk.ContainsString(openapiPaths, r.URL.Path) {
			_, _ = w.Write(restapi.SwaggerJSON)
			return
		}
		next.ServeHTTP(w, r)
	})
}
