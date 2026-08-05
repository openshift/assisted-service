package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/metrics"
	ctxparams "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// LocalAuthzHandler is the authorizer middleware for AUTH_TYPE=local.
// When resource-scope enforcement is enabled, it verifies that scoped
// tokens (those carrying a resource claim) can only access the specific
// resource identified by their claim. Unscoped requests (e.g. agentAuth)
// are permitted without resource verification.
//
// Endpoints that use query parameters rather than path parameters (e.g.
// /v2/events) must enforce their own scope check in the handler itself.
type LocalAuthzHandler struct {
	log          logrus.FieldLogger
	enforceScope bool
	schemeFor    func(*http.Request) string
	metricsAPI   metrics.API
}

func NewLocalAuthzHandler(cfg *Config, log logrus.FieldLogger) *LocalAuthzHandler {
	return &LocalAuthzHandler{
		log:          log,
		enforceScope: cfg.LocalAuthEnforceResourceScope,
		schemeFor:    authSchemeFromRoute,
	}
}

func authSchemeFromRoute(r *http.Request) string {
	route := middleware.MatchedRouteFrom(r)
	if route == nil {
		return ""
	}
	return route.Authenticator.Schemes[0]
}

func (h *LocalAuthzHandler) CreateAuthorizer() func(*http.Request) error {
	return h.authorizerMiddleware
}

func (h *LocalAuthzHandler) IsAdmin(ctx context.Context) bool {
	if !h.enforceScope {
		return true
	}
	payload := ocm.PayloadFromContext(ctx)
	// Scoped tokens are not admin — they should not bypass OwnedBy filters.
	if payload.ResourceType != "" {
		return false
	}
	return true
}

func (h *LocalAuthzHandler) OwnedBy(_ context.Context, db *gorm.DB) *gorm.DB {
	return db
}

func (h *LocalAuthzHandler) OwnedByUser(_ context.Context, db *gorm.DB, _ string) *gorm.DB {
	return db
}

func (h *LocalAuthzHandler) HasAccessTo(_ context.Context, _ interface{}, _ Action) (bool, error) {
	return true, nil
}

func (h *LocalAuthzHandler) HasOrgBasedCapability(_ context.Context, _ string) (bool, error) {
	return true, nil
}

func (h *LocalAuthzHandler) SetMetrics(metricsAPI metrics.API) {
	h.metricsAPI = metricsAPI
}

func (h *LocalAuthzHandler) authorizerMiddleware(request *http.Request) error {
	authScheme := h.schemeFor(request)

	// Only enforce resource scoping for urlAuth requests.
	if authScheme != "urlAuth" {
		return nil
	}

	if !h.enforceScope {
		return nil
	}

	payload := request.Context().Value(restapi.AuthKey)
	if payload == nil {
		return common.NewApiError(http.StatusUnauthorized, fmt.Errorf("payload missing from authenticated context"))
	}
	authPayload, ok := payload.(*ocm.AuthPayload)
	if !ok {
		return common.NewApiError(http.StatusUnauthorized, fmt.Errorf("malformed auth payload"))
	}

	if authPayload.ResourceType == "" || authPayload.ResourceID == "" {
		return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
	}

	return h.verifyResourceScope(request, authPayload)
}

func (h *LocalAuthzHandler) verifyResourceScope(request *http.Request, payload *ocm.AuthPayload) error {
	ctx := request.Context()
	infraEnvID := ctxparams.GetParam(ctx, ctxparams.InfraEnvId)
	clusterID := ctxparams.GetParam(ctx, ctxparams.ClusterId)

	if infraEnvID == "" && clusterID == "" {
		return nil
	}

	switch payload.ResourceType {
	case gencrypto.InfraEnvKey:
		if infraEnvID != "" && payload.ResourceID == infraEnvID {
			h.recordScopeCheck(payload.ResourceType, "allowed")
			return nil
		}
		h.log.WithFields(logrus.Fields{
			"token_resource_type":  payload.ResourceType,
			"token_resource_id":    payload.ResourceID,
			"request_infra_env_id": infraEnvID,
		}).Warn("URL auth resource scope mismatch")
		h.recordScopeCheck(payload.ResourceType, "denied")
		return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))

	case gencrypto.ClusterKey:
		if clusterID != "" && payload.ResourceID == clusterID {
			h.recordScopeCheck(payload.ResourceType, "allowed")
			return nil
		}
		h.log.WithFields(logrus.Fields{
			"token_resource_type": payload.ResourceType,
			"token_resource_id":   payload.ResourceID,
			"request_cluster_id":  clusterID,
		}).Warn("URL auth resource scope mismatch")
		h.recordScopeCheck(payload.ResourceType, "denied")
		return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))

	default:
		h.log.WithField("token_resource_type", payload.ResourceType).
			Error("URL auth token has unrecognized resource type")
		h.recordScopeCheck(payload.ResourceType, "denied")
		return common.NewApiError(http.StatusUnauthorized, fmt.Errorf("unrecognized resource type in urlAuth token"))
	}
}

func (h *LocalAuthzHandler) recordScopeCheck(resourceType gencrypto.LocalJWTKeyType, result string) {
	if h.metricsAPI != nil {
		h.metricsAPI.URLAuthScopeCheck(string(resourceType), "path", result)
	}
}
