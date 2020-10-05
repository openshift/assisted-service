package context

import (
	"context"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
)

type contextKey string

//path parameters that are saved on the context
const (
	ClusterId = "cluster_id"
	HostId    = "host_id"
)

func GetParam(ctx context.Context, key string) string {
	val := ctx.Value(contextKey(key))
	if val == nil {
		return ""
	} else {
		return val.(string)
	}
}

//openapi middleware handler that takes matched params from the route and put them
//on the context for further use (mainly by logs). parameters are prefixed to avoid
//conflicts (for example: cluster_id --> PARAM$cluster_id)
func ContextHandler() func(http.Handler) http.Handler {

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			//extract the Route from the request
			mr := middleware.MatchedRouteFrom(r)
			//set the parameters on the request's context
			ctx := r.Context()
			if mr != nil {
				for _, p := range mr.Params {
					ctx = context.WithValue(ctx, contextKey(p.Name), p.Value)
				}
				r = r.WithContext(ctx)
			}

			//pass control to the next handler in the chain
			next.ServeHTTP(w, r)
		})
	}
}
