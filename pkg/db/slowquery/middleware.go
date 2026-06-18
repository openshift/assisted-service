package slowquery

import (
	"fmt"
	"net/http"
	"strings"

	rmiddleware "github.com/go-openapi/runtime/middleware"
)

// Middleware annotates HTTP requests with the HTTP slow-query scope for matching routes.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.scopeEnabled(ScopeHTTP) {
				next.ServeHTTP(w, r)
				return
			}

			route := routeLabel(r)
			if !cfg.matchesHTTPRoute(route) {
				next.ServeHTTP(w, r)
				return
			}

			SetGoroutineScope(ScopeHTTP, route)
			defer ClearGoroutineScope()

			ctx := WithRoute(WithScope(r.Context(), ScopeHTTP), route)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func routeLabel(r *http.Request) string {
	if mr := rmiddleware.MatchedRouteFrom(r); mr != nil {
		if mr.Operation != nil && mr.Operation.ID != "" {
			return mr.Operation.ID
		}
		if mr.PathPattern != "" {
			return fmt.Sprintf("%s %s", strings.ToUpper(r.Method), mr.PathPattern)
		}
	}
	return fmt.Sprintf("%s %s", strings.ToUpper(r.Method), r.URL.Path)
}
