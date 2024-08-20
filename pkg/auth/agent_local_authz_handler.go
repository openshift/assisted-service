package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-openapi/runtime/middleware"
	"github.com/golang-jwt/jwt/v4"
	"github.com/openshift/assisted-service/internal/common"
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

	case "agentAuth", "userAuth", "watcherAuth":
		return a.agentInstallerAuthorizer(request, authScheme)
	default:
		return errors.New("unsupported auth scheme")
	}
}

func JWTMiddleware(request *http.Request, authScheme string) (jwt.MapClaims, error) {
	// if authScheme is userAuth then you get the Authorization header
	// else get different
	logrus.Infof("******** authScheme = %s", authScheme)
	var authHeader string
	switch authScheme {
	case "agentAuth":
		authHeader = request.Header.Get("X-Secret-Key")
	case "userAuth":
		authHeader = request.Header.Get("Authorization")
	case "watcherAuth":
		authHeader = request.Header.Get("Watcher-Authorization")
	default:
		authHeader = ""
	}
	logrus.Infof("******** authHeader = %s", authHeader)
	if authHeader == "" {
		return nil, errors.New("**** missing Authorization header")
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	logrus.Infof("***** tokenString=%s", tokenString)

	// Parse the JWT token without validating it
	claims := jwt.MapClaims{}
	// use public key
	_, _, err := new(jwt.Parser).ParseUnverified(tokenString, claims)

	return claims, err

}

func (a *AgentLocalAuthzHandler) agentInstallerAuthorizer(request *http.Request, authScheme string) error {
	claims, err := JWTMiddleware(request, authScheme)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("******* claims error: %s", err))
	}

	authClaim, ok := claims["sub"].(string)
	if !ok {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("******* malformed sub claim"))
	}

	if authClaim == "" || authScheme == "" {
		return common.NewApiError(http.StatusBadRequest, fmt.Errorf("******* token missing authClaim or endpoint is missing authScheme"))
	}
	if authClaim != authScheme {
		return common.NewInfraError(http.StatusForbidden, fmt.Errorf("******* authClaim %s is unauthorized to access. authScheme =%s", authClaim, authScheme))
	}
	logrus.Infof("********** AUTHZN PASSED! authClaim=%s ", authClaim)

	return nil
}
