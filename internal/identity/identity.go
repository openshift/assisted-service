package identity

import (
	"context"
	"fmt"

	"github.com/openshift/assisted-service/pkg/auth"
)

func IsAdmin(ctx context.Context) bool {
	authPayload := auth.PayloadFromContext(ctx)
	if authPayload == nil {
		return false
	}

	return authPayload.IsAdmin
}

func AddUserFilter(ctx context.Context, query string) string {
	if !IsAdmin(ctx) {
		if query != "" {
			query += " and "
		}
		username := auth.UserNameFromContext(ctx)
		query += fmt.Sprintf("user_name = '%s'", username)
	}
	return query
}
