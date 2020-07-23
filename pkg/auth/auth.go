package auth

import (
	"context"
	"net/http"
)

type contextKey string

const contextUserIDKey = contextKey("user_id")
const contextOrgIDKey = contextKey("org_id")
const contextRoleKey = contextKey("role")
const AdminUserRole = "admin"
const DefaultUserID = "0000000"
const DefaultOrgID = "0000000"

// Fake auth Middleware handler to add username from headers to request context
func GetUserInfoMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Update the context, as the jwt middleware will update it
		ctx := r.Context()
		ctx = UserIDToContext(ctx, DefaultUserID)
		ctx = OrgIDToContext(ctx, DefaultOrgID)
		ctx = UserRoleToContext(ctx, AdminUserRole)
		*r = *r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

func UserIDFromContext(ctx context.Context) string {
	userID := ctx.Value(contextUserIDKey)
	if userID == nil {
		userID = ""
	}
	return userID.(string)
}

func UserIDToContext(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, contextUserIDKey, userID)
}

func OrgIDFromContext(ctx context.Context) string {
	orgID := ctx.Value(contextOrgIDKey)
	if orgID == nil {
		orgID = ""
	}
	return orgID.(string)
}

func OrgIDToContext(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, contextOrgIDKey, orgID)
}

func UserRoleFromContext(ctx context.Context) string {
	role := ctx.Value(contextRoleKey)
	if role == nil {
		role = ""
	}
	return role.(string)
}

func UserRoleToContext(ctx context.Context, roleID string) context.Context {
	return context.WithValue(ctx, contextRoleKey, roleID)
}
