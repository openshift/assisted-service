package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/golang-jwt/jwt/v4"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/restapi"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

/* AgentLocalAuthzHandler is the authorizer middleware that is being used for
 * RHSSO authentication cases and work with AMS to ensure tenancy
 * and user based access policies
 */
type AgentLocalAuthzHandler struct {
	cfg *Config
	log logrus.FieldLogger
	db  *gorm.DB
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

	case "agentAuth":
		logrus.Infof("********** AgentLocalAuthzHandler authScheme=%s", authScheme)
		return a.agentInstallerAuthorizer(request.Context(), authScheme)
	default:
		return nil
	}
}

func (a *AgentLocalAuthzHandler) agentInstallerAuthorizer(ctx context.Context, authScheme string) error {
	logrus.Infof("********** restapi.AuthKey=%s", restapi.AuthKey)
	payload := ctx.Value(restapi.AuthKey)
	if payload == nil {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("******* payload missing from authenticated context"))
	}

	claims, ok := payload.(jwt.MapClaims)
	if !ok {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("******* malformed claims payload"))
	}

	authClaim, ok := claims["sub"].(string)
	logrus.Infof("********** authClaim=%s. expected is agentAuth", authClaim)
	if !ok {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("******* malformed sub claim"))
	}

	// requestID := params.GetParam(ctx, params.InfraEnvId)
	if authClaim == "" || authScheme == "" {
		return common.NewApiError(http.StatusBadRequest, fmt.Errorf("******* token missing authClaim or endpoint is missing authScheme"))
	}
	if authClaim != authScheme {
		return common.NewInfraError(http.StatusForbidden, fmt.Errorf("******* authClaim %s is unauthorized to access", authClaim))
	}
	logrus.Infof("********** AUTHZN PASSED! authClaim=%s. expected is agentAuth", authClaim)

	return nil
}
