package identity

import (
	"context"
	"fmt"

	"github.com/filanov/bm-inventory/pkg/auth"
)

func IsAdmin(ctx context.Context) bool {
	return auth.UserRoleFromContext(ctx) == auth.AdminUserRole
}

func GetUserIDFilter(ctx context.Context) string {
	query := ""
	if !IsAdmin(ctx) {
		user_id := auth.UserIDFromContext(ctx)
		query = fmt.Sprintf("user_id = '%s'", user_id)
	}
	return query
}
