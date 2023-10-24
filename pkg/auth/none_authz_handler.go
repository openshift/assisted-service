package auth

import (
	"context"
	"net/http"

	"gorm.io/gorm"
)

/*
NoneHandler is the authorizer middleware that is being used for

	non-RHSSO authentication cases. It will basically authorize any
	request, as there is no user and tenancy based concepts in these
	cases
*/
type NoneHandler struct {
}

func (*NoneHandler) CreateAuthorizer() func(*http.Request) error {
	return func(*http.Request) error {
		return nil
	}
}

func (*NoneHandler) OwnedBy(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db
}

func (*NoneHandler) OwnedByUser(ctx context.Context, db *gorm.DB, username string) *gorm.DB {
	return db
}

func (*NoneHandler) HasAccessTo(ctx context.Context, obj interface{}, action Action) (bool, error) {
	return true, nil
}

func (*NoneHandler) IsAdmin(ctx context.Context) bool {
	return true
}

func (*NoneHandler) HasOrgBasedCapability(ctx context.Context, capability string) (bool, error) {
	return true, nil
}
