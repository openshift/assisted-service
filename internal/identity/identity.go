package identity

import (
	"context"
	"fmt"

	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/thoas/go-funk"

	"github.com/openshift/assisted-service/pkg/auth"
)

func IsAdmin(ctx context.Context) bool {
	authPayload := auth.PayloadFromContext(ctx)
	allowedRoles := []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole}
	return funk.Contains(allowedRoles, authPayload.Role)
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
