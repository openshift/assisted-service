package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	params "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

type AuthzHandler struct {
	Enabled bool
	log     logrus.FieldLogger
	client  *ocm.Client
	db      *gorm.DB
}

func NewAuthzHandler(cfg *Config, ocmCLient *ocm.Client, log logrus.FieldLogger, db *gorm.DB) *AuthzHandler {
	a := &AuthzHandler{
		Enabled: cfg.AuthType == TypeRHSSO,
		client:  ocmCLient,
		log:     log,
		db:      db,
	}
	return a
}

// CreateAuthorizer returns Authorizer if auth is enabled
func (a *AuthzHandler) CreateAuthorizer() func(*http.Request) error {
	if !a.Enabled {
		return func(*http.Request) error {
			return nil
		}
	}

	return a.Authorizer
}

func (a *AuthzHandler) isObjectOwnedByUser(id string, obj interface{}, username string) (bool, error) {
	if a.db != nil {
		err := a.db.First(obj, "id = ? and user_name = ?", id, username).Error
		if err != nil {
			//if user is not the owner of the object return false
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			//in case of a real db error, indicate it to the caller
			return false, err
		}
	}

	return true, nil
}

// Authorizer is used to authorize a request after the Auth function was called using the "Auth*" functions
// and the principal was stored in the context in the "AuthKey" context value.
func (a *AuthzHandler) Authorizer(request *http.Request) error {
	payload := ocm.PayloadFromContext(request.Context())
	username := payload.Username

	if ok := a.hasSufficientRole(request, payload); !ok {
		return common.NewInfraError(
			http.StatusForbidden,
			fmt.Errorf(
				"%s: Unauthorized to access route (insufficient role %s)",
				username, payload.Role))
	}

	var isAuthorized, existInCache bool
	var err error
	defer func() {
		payload.IsAuthorized = isAuthorized
		if !existInCache && shouldStorePayloadInCache(err) {
			a.client.Cache.Set(username, payload, cache.DefaultExpiration)
		}
	}()

	if payload, existInCache := a.client.Cache.Get(username); existInCache {
		isAuthorized = payload.(*ocm.AuthPayload).IsAuthorized
	} else {
		if isAuthorized, err = a.allowedToUseAssistedInstaller(username); err != nil {
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	if !isAuthorized {
		return common.NewInfraError(
			http.StatusUnauthorized,
			fmt.Errorf(
				"%s: Unauthorized to access route (access review failed)",
				username))
	}

	if payload.Role == ocm.UserRole {
		ownedBy := true
		if clusterID := params.GetParam(request.Context(), params.ClusterId); clusterID != "" {
			ownedBy, err = a.isObjectOwnedByUser(clusterID, &common.Cluster{}, username)
		} else if infraEnvID := params.GetParam(request.Context(), params.InfraEnvId); infraEnvID != "" {
			ownedBy, err = a.isObjectOwnedByUser(infraEnvID, &common.InfraEnv{}, username)
		}
		if err != nil {
			a.log.Errorf("Failed to verify access to object. Error %v", err)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
		if !ownedBy {
			return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
		}
	}

	return nil
}

func (a *AuthzHandler) allowedToUseAssistedInstaller(username string) (bool, error) {
	return a.client.Authorization.AccessReview(
		context.Background(), username, ocm.AMSActionCreate, ocm.BareMetalClusterResource)
}

func (a *AuthzHandler) hasSufficientRole(
	request *http.Request,
	payload *ocm.AuthPayload) bool {

	route := middleware.MatchedRouteFrom(request)

	allScopesAreAllowedResponse := func() bool {
		a.log.Debugf(
			"%s: Authorized user: %s all roles are allowed",
			route.PathPattern, payload.Username)
		return true
	}

	if route.Authenticators == nil {
		return allScopesAreAllowedResponse()
	}

	authScheme := route.Authenticator.Schemes[0]
	for _, policy := range route.Authenticators {
		policyScopes, exist := policy.Scopes[authScheme]
		if !exist {
			continue
		}
		if len(policyScopes) == 0 {
			return allScopesAreAllowedResponse()
		}
		if funk.Contains(policyScopes, string(payload.Role)) {
			a.log.Debugf(
				"%s: Authorized user: %s for role: %s",
				route.PathPattern, payload.Username, payload.Role)
			return true
		}
	}
	a.log.Warnf(
		"Unauthorized user %s: insufficient role: %s allowed roles: %q",
		payload.Username,
		payload.Role,
		route.Authenticator.Scopes)
	return false
}
