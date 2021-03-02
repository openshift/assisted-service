package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

type AuthzHandler struct {
	Enabled bool
	log     logrus.FieldLogger
	client  *ocm.Client
}

func NewAuthzHandler(cfg *Config, ocmCLient *ocm.Client, log logrus.FieldLogger) *AuthzHandler {
	a := &AuthzHandler{
		Enabled: cfg.ResolvedAuthType() == TypeRHSSO,
		client:  ocmCLient,
		log:     log,
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

// Authorizer is used to authorize a request after the Auth function was called using the "Auth*" functions
// and the principal was stored in the context in the "AuthKey" context value.
func (a *AuthzHandler) Authorizer(request *http.Request) (err error) {
	payload := ocm.PayloadFromContext(request.Context())
	username := payload.Username

	if ok := a.hasSufficientRole(request, payload); !ok {
		return common.NewInfraError(
			http.StatusUnauthorized,
			fmt.Errorf(
				"%s: Unauthorized to access route (insufficient role %s)",
				username, payload.Role))
	}

	var isAuthorized, existInCache bool
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
			return common.NewInfraError(http.StatusInternalServerError, err)
		}
	}

	if !isAuthorized {
		return common.NewInfraError(
			http.StatusUnauthorized,
			fmt.Errorf(
				"%s: Unauthorized to access route (access review failed)",
				username))
	}
	return
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
