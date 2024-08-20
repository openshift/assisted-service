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
 * agent based installer (ABI) authentication cases
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
	var authHeader string
	switch authScheme {
	// Agent authentication works with the "Authorization" header, but we explicitly set the "X-Secret-Key" header
	// as it's the recommended header for assisted-installer-agent. The choice of header depends on the annotations
	// in the swagger.yaml for the specific endpoint.
	// For endpoints tagged with both agentAuth and userAuth, either "X-Secret-Key" or "Authorization" can be used.
	// However, for ABI, we assume the three different user personas (agentAuth, userAuth, watcherAuth) have distinct roles.
	// Therefore, we generate separate tokens for each persona and select the appropriate token based on the header.
	// AuthAgentAuth could function with the "Authorization" header and the AuthUserAuth token if both agentAuth
	// and userAuth are defined for the same endpoint.
	case "agentAuth":
		// AuthAgentAuth Applies when the "X-Secret-Key" header is set
		// Refer assisted-installer-agent codebase
		// used by agent service
		authHeader = request.Header.Get("X-Secret-Key")
	case "userAuth":
		// AuthUserAuth Applies when the "Authorization" header is set
		// used by ABI'S systemd services
		authHeader = request.Header.Get("Authorization")
	case "watcherAuth":
		// AuthWatcherAuth Applies when the "Watcher-Authorization" header is set
		// used by ABI's wait-for and monitor commands
		authHeader = request.Header.Get("Watcher-Authorization")
	default:
		authHeader = ""
	}
	if authHeader == "" {
		return nil, errors.New("missing authorization header")
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")

	claims := jwt.MapClaims{}
	_, _, err := new(jwt.Parser).ParseUnverified(tokenString, claims)

	return claims, err
}

func (a *AgentLocalAuthzHandler) agentInstallerAuthorizer(request *http.Request, authScheme string) error {
	claims, err := JWTMiddleware(request, authScheme)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("claims error: %s", err))
	}

	authClaim, ok := claims["sub"].(string)
	if !ok {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("malformed sub claim"))
	}

	if authClaim == "" || authScheme == "" {
		return common.NewApiError(http.StatusBadRequest, fmt.Errorf("token missing authClaim or endpoint is missing authScheme"))
	}
	if authClaim != authScheme {
		return common.NewInfraError(http.StatusForbidden, fmt.Errorf("authClaim %s is unauthorized to access. authScheme =%s", authClaim, authScheme))
	}
	return nil
}
