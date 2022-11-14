package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type Action string

const ReadAction Action = "read"
const UpdateAction Action = "update"
const DeleteAction Action = "delete"
const NoneAction Action = "none"

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

	/* verify that the current user has access rights (depending on the requested)
	 * action) to the input resource
	 */
	HasAccessTo(ctx context.Context, obj interface{}, action Action) (bool, error)

	/* Provides the middleware authorization algorithm */
	CreateAuthorizer() func(*http.Request) error

	/* Returns true if the user has an admin role  */
	IsAdmin(ctx context.Context) bool

	/* verify that the current user has a capability (based on their organization capabilities)  */
	HasOrgBasedCapability(ctx context.Context, capability string) (bool, error)
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

func ValidateAccessToMultiarch(ctx context.Context, authzHandler Authorizer) error {
	var err error
	var multiarchAllowed bool

	multiarchAllowed, err = authzHandler.HasOrgBasedCapability(ctx, ocm.MultiarchCapabilityName)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("error getting user %s capability, error: %w", ocm.MultiarchCapabilityName, err))
	}
	if !multiarchAllowed {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("%s", "multiarch clusters are not available"))
	}
	return nil
}
