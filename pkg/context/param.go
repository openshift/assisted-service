package context

import (
	"context"
	"net/http"
	"runtime"
	"strings"

	"github.com/go-openapi/runtime/middleware"
)

type contextKey string

// path parameters that are saved on the context
const (
	ClusterId  = "cluster_id"
	HostId     = "host_id"
	InfraEnvId = "infra_env_id"
)

func GetParam(ctx context.Context, key string) string {
	val := ctx.Value(contextKey(key))
	if val == nil {
		return ""
	} else {
		return val.(string)
	}
}

func SetParam(ctx context.Context, key string, value interface{}) context.Context {
	return context.WithValue(ctx, contextKey(key), value)
}

func Copy(ctx context.Context) context.Context {
	newContext := context.Background()
	for k, v := range GetContextParams(ctx) {
		newContext = SetParam(newContext, k, v)
	}
	return newContext
}

// return the values of interest (goid and path parameters) that we
// are saving on the context
func GetContextParams(ctx context.Context) map[string]interface{} {
	var fields = make(map[string]interface{})
	fields["go-id"] = goid()

	cluster_id := GetParam(ctx, ClusterId)
	if cluster_id != "" {
		fields[ClusterId] = cluster_id
	}

	host_id := GetParam(ctx, HostId)
	if host_id != "" {
		fields[HostId] = host_id
	}

	infra_env := GetParam(ctx, InfraEnvId)
	if infra_env != "" {
		fields[InfraEnvId] = infra_env
	}
	return fields
}

// get the low-level gorouting id
// This has been taken from:
// https://groups.google.com/d/msg/golang-nuts/Nt0hVV_nqHE/bwndAYvxAAAJ
// This is hacky and should not be used for anything but logging
func goid() string {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	return idField
}

// openapi middleware handler that takes matched params from the route and put them
// on the context for further use (mainly by logs). parameters are prefixed to avoid
// conflicts (for example: cluster_id --> PARAM$cluster_id)
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
