package matchedRouteContext

import (
	"context"

	rmiddleware "github.com/go-openapi/runtime/middleware"
)

type ctxKey int8

const (
	_ ctxKey = iota
	ctxMatchedRoute
	ctxMethod
)

func FromContext(ctx context.Context) (matchedRoute *rmiddleware.MatchedRoute, method string) {
	matchedRoute = nil
	method = ""
	m := ctx.Value(ctxMatchedRoute)
	if m != nil {
		mm := m.(rmiddleware.MatchedRoute)
		matchedRoute = &mm
	}
	m = ctx.Value(ctxMethod)
	if m != nil {
		method = m.(string)
	}
	return matchedRoute, method
}

func ToContext(ctx context.Context, matchedRoute *rmiddleware.MatchedRoute, method string) context.Context {
	c := context.WithValue(ctx, ctxMatchedRoute, *matchedRoute)
	return context.WithValue(c, ctxMethod, method)
}
