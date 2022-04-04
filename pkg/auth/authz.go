package auth

import (
	"context"
	"net/http"

	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type Authorizer interface {
	/* Limits the database query to access records that are owned by the current user,
	 * according to the configured access policy.
	 */
	OwnedBy(ctx context.Context, db *gorm.DB) *gorm.DB

	/* Limits the database query to access records owned only by the input user,
	 * regardless of the configured access policy. If user-based authentication
	 * is not supported, the function effectively will not limit access.
	 */
	OwnedByUser(ctx context.Context, db *gorm.DB, username string) *gorm.DB

	/* Provides the middleware authorization algorithm */
	CreateAuthorizer() func(*http.Request) error
}

func NewAuthzHandler(cfg *Config, ocmCLient *ocm.Client, log logrus.FieldLogger, db *gorm.DB) Authorizer {
	if cfg.AuthType == TypeRHSSO {
		return &AuthzHandler{
			cfg:    cfg,
			client: ocmCLient,
			log:    log,
			db:     db,
		}
	}
	return &NoneHandler{}
}
