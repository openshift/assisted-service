package identity

import (
	"context"
	"fmt"

	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/thoas/go-funk"
)

func IsAdmin(ctx context.Context) bool {
	authPayload := ocm.PayloadFromContext(ctx)
	allowedRoles := []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole}
	return funk.Contains(allowedRoles, authPayload.Role)
}

func AddUserFilter(ctx context.Context, query string) string {
	if !IsAdmin(ctx) {
		if query != "" {
			query += " and "
		}
		username := ocm.UserNameFromContext(ctx)
		query += fmt.Sprintf("user_name = '%s'", username)
	}
	return query
}
