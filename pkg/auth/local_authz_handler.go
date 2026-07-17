package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	params "github.com/openshift/assisted-service/pkg/context"
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
type LocalAuthzHandler struct {
	log                logrus.FieldLogger
	db                 *gorm.DB
	enforceScope       bool
	fallbackPermissive bool
}

func NewLocalAuthzHandler(cfg *Config, log logrus.FieldLogger, db *gorm.DB) *LocalAuthzHandler {
	return &LocalAuthzHandler{
		log:                log,
		db:                 db,
		enforceScope:       cfg.LocalAuthEnforceResourceScope,
		fallbackPermissive: !cfg.LocalAuthEnforceResourceScope,
	}
}

func (h *LocalAuthzHandler) CreateAuthorizer() func(*http.Request) error {
	return h.authorizerMiddleware
}

func (h *LocalAuthzHandler) IsAdmin(ctx context.Context) bool {
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

func (h *LocalAuthzHandler) authorizerMiddleware(request *http.Request) error {
	route := middleware.MatchedRouteFrom(request)
	authScheme := route.Authenticator.Schemes[0]

	// Only enforce resource scoping for urlAuth requests.
	if authScheme != "urlAuth" {
		return nil
	}

	if !h.enforceScope {
		return nil
	}

	payload := request.Context().Value(restapi.AuthKey)
	if payload == nil {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("payload missing from authenticated context"))
	}
	authPayload, ok := payload.(*ocm.AuthPayload)
	if !ok {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("malformed auth payload"))
	}

	return h.verifyResourceScope(request, authPayload)
}

func (h *LocalAuthzHandler) verifyResourceScope(request *http.Request, payload *ocm.AuthPayload) error {
	ctx := request.Context()

	// Check path-parameter based endpoints first.
	if infraEnvID := params.GetParam(ctx, params.InfraEnvId); infraEnvID != "" {
		if payload.ResourceType != string(gencrypto.InfraEnvKey) || payload.ResourceID != infraEnvID {
			h.log.Warnf("Resource scope mismatch: token %s=%s, request infra_env_id=%s",
				payload.ResourceType, payload.ResourceID, infraEnvID)
			return common.NewApiError(http.StatusForbidden, fmt.Errorf("Object Not Found"))
		}
		return nil
	}

	if clusterID := params.GetParam(ctx, params.ClusterId); clusterID != "" {
		if payload.ResourceType != string(gencrypto.ClusterKey) || payload.ResourceID != clusterID {
			h.log.Warnf("Resource scope mismatch: token %s=%s, request cluster_id=%s",
				payload.ResourceType, payload.ResourceID, clusterID)
			return common.NewApiError(http.StatusForbidden, fmt.Errorf("Object Not Found"))
		}
		return nil
	}

	// Events endpoint: no path params, uses query params.
	return h.verifyEventsScopeFromQuery(request, payload)
}

func (h *LocalAuthzHandler) verifyEventsScopeFromQuery(request *http.Request, payload *ocm.AuthPayload) error {
	query := request.URL.Query()
	qClusterID := query.Get("cluster_id")
	qInfraEnvID := query.Get("infra_env_id")
	qHostID := query.Get("host_id")

	switch payload.ResourceType {
	case string(gencrypto.InfraEnvKey):
		if qInfraEnvID != "" {
			if payload.ResourceID != qInfraEnvID {
				h.log.Warnf("Events scope mismatch: token infra_env_id=%s, query infra_env_id=%s",
					payload.ResourceID, qInfraEnvID)
				return common.NewApiError(http.StatusForbidden, fmt.Errorf("Object Not Found"))
			}
			return nil
		}
		if qHostID != "" {
			return h.verifyHostBelongsToInfraEnv(payload.ResourceID, qHostID)
		}
		// infra_env_id token with no matching filter (e.g. querying by cluster_id)
		h.log.Warnf("Events scope rejected: infra_env_id token cannot query events without infra_env_id or host_id filter")
		return common.NewApiError(http.StatusForbidden, fmt.Errorf("Object Not Found"))

	case string(gencrypto.ClusterKey):
		if qClusterID != "" {
			if payload.ResourceID != qClusterID {
				h.log.Warnf("Events scope mismatch: token cluster_id=%s, query cluster_id=%s",
					payload.ResourceID, qClusterID)
				return common.NewApiError(http.StatusForbidden, fmt.Errorf("Object Not Found"))
			}
			return nil
		}
		// cluster_id token with no matching filter
		h.log.Warnf("Events scope rejected: cluster_id token cannot query events without cluster_id filter")
		return common.NewApiError(http.StatusForbidden, fmt.Errorf("Object Not Found"))
	}

	return common.NewApiError(http.StatusForbidden, fmt.Errorf("Object Not Found"))
}

func (h *LocalAuthzHandler) verifyHostBelongsToInfraEnv(infraEnvID, hostID string) error {
	var count int64
	err := h.db.Table("hosts").
		Where("id = ? AND infra_env_id = ?", hostID, infraEnvID).
		Count(&count).Error
	if err != nil {
		h.log.WithError(err).Errorf("Failed to verify host %s belongs to infra_env %s", hostID, infraEnvID)
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("failed to verify host ownership"))
	}
	if count == 0 {
		h.log.Warnf("Host %s does not belong to infra_env %s", hostID, infraEnvID)
		return common.NewApiError(http.StatusForbidden, fmt.Errorf("Object Not Found"))
	}
	return nil
}
