package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

const (
	bareMetalClusterResource string = "BareMetalCluster"
	amsActionCreate          string = "create"
	capabilityName           string = "bare_metal_installer_admin"
	capabilityType           string = "Account"

	// AdminUsername for disabled auth
	AdminUsername string = "admin"
)

type AuthzHandler struct {
	EnableAuth  bool
	EnableAuthz bool
	log         logrus.FieldLogger
	client      *ocm.Client
}

func NewAuthzHandler(cfg Config, ocmCLient *ocm.Client, log logrus.FieldLogger) *AuthzHandler {
	a := &AuthzHandler{
		EnableAuth:  cfg.EnableAuth,
		EnableAuthz: cfg.EnableAuthz,
		client:      ocmCLient,
		log:         log,
	}
	return a
}

// CreateAuthorizer returns Authorizer if auth is enabled
func (a *AuthzHandler) CreateAuthorizer() func(*http.Request) error {
	if !a.EnableAuthz {
		return func(*http.Request) error {
			return nil
		}
	}

	return a.Authorizer
}

// Authorizer is used to authorize a request after the Auth function was called using the "Auth*" functions
// and the principal was stored in the context in the "AuthKey" context value.
func (a *AuthzHandler) Authorizer(request *http.Request) error {
	if !a.EnableAuth {
		return nil
	}
	payload := PayloadFromContext(request.Context())
	username := payload.Username
	payloadFromCache, found := a.client.Cache.Get(username)
	if found {
		// Update payload with values from cache
		payloadFromCache := payloadFromCache.(*ocm.AuthPayload)
		payload.IsUser = payloadFromCache.IsUser
	} else {
		// Inquire AMS for user's role
		allowed, err := a.allowedToUseAssistedInstaller(username)
		if err != nil {
			a.log.Errorf("Failed to authorize user: %v", err)
		} else {
			payload.IsUser = allowed
			a.client.Cache.Set(username, payload, cache.DefaultExpiration)
		}
	}

	if payload.IsUser {
		// authorized user
		return nil
	}

	return fmt.Errorf("method is not allowed")
}

// Ensure that the user has authorization to use the bare metal installer service.
// For now the indication is simply "create BareMetalCluster" permission,
// which is allowed for users with BareMetalInstallerUser role.
func (a *AuthzHandler) allowedToUseAssistedInstaller(username string) (bool, error) {
	return a.client.Authorization.AccessReview(
		context.Background(), username, amsActionCreate, bareMetalClusterResource)
}

// PayloadFromContext returns auth payload from the specified context
func PayloadFromContext(ctx context.Context) *ocm.AuthPayload {
	payload := ctx.Value(restapi.AuthKey)
	if payload == nil {
		// fallback to system-admin
		return &ocm.AuthPayload{IsAdmin: true, Username: AdminUsername}
	}
	return payload.(*ocm.AuthPayload)
}

// UserNameFromContext returns username from the specified context
func UserNameFromContext(ctx context.Context) string {
	payload := PayloadFromContext(ctx)
	return payload.Username
}

// OrgIDFromContext returns org ID from the specified context
func OrgIDFromContext(ctx context.Context) string {
	payload := PayloadFromContext(ctx)
	return payload.Organization
}
