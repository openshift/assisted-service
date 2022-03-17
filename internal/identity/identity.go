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

func AddOwnerFilter(ctx context.Context, query string, filterByOrg bool) string {
	if IsAdmin(ctx) {
		return query
	}
	if filterByOrg {
		if query != "" {
			query += " and "
		}
		orgId := ocm.OrgIDFromContext(ctx)
		query += fmt.Sprintf("org_id = '%s'", orgId)
		return query
	}
	return AddUserFilter(ctx, query)
}
