package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/golang-jwt/jwt/v4"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/restapi"
	"gorm.io/gorm"
)

/* AgentLocalAuthzHandler is the authorizer middleware that is being used for
 * agent based installer (ABI) authentication cases
 */
type AgentLocalAuthzHandler struct {
}

func (a *AgentLocalAuthzHandler) CreateAuthorizer() func(*http.Request) error {
	return a.authorizerMiddleware
}

func (a *AgentLocalAuthzHandler) IsAdmin(ctx context.Context) bool {
	return true
}

func (a *AgentLocalAuthzHandler) OwnedBy(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db
}

func (a *AgentLocalAuthzHandler) OwnedByUser(ctx context.Context, db *gorm.DB, username string) *gorm.DB {
	return db
}

func (a *AgentLocalAuthzHandler) HasAccessTo(ctx context.Context, obj interface{}, action Action) (bool, error) {
	return true, nil
}

func (a *AgentLocalAuthzHandler) HasOrgBasedCapability(ctx context.Context, capability string) (bool, error) {
	return true, nil
}

func (a *AgentLocalAuthzHandler) authorizerMiddleware(request *http.Request) error {
	route := middleware.MatchedRouteFrom(request)
	switch authScheme := route.Authenticator.Schemes[0]; authScheme {

	case "agentAuth", "userAuth", "watcherAuth":
		return a.agentInstallerAuthorizer(request, authScheme)
	default:
		return errors.New("unsupported auth scheme")
	}
}

func (a *AgentLocalAuthzHandler) agentInstallerAuthorizer(request *http.Request, authScheme string) error {
	payload := request.Context().Value(restapi.AuthKey)
	if payload == nil {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("payload missing from authenticated context"))
	}

	claims, ok := payload.(jwt.MapClaims)
	if !ok {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("malformed claims payload"))
	}

	authClaim, ok := claims["auth_scheme"].(string)
	if !ok {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("malformed auth_scheme claim"))
	}

	if authClaim == "" || authScheme == "" {
		return common.NewApiError(http.StatusBadRequest, fmt.Errorf("token missing authClaim or endpoint is missing authScheme"))
	}
	if authClaim != authScheme {
		return common.NewInfraError(http.StatusForbidden, fmt.Errorf("authClaim %s is unauthorized to access", authClaim))
	}
	return nil
}
