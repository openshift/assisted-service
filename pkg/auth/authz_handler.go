package auth

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

const (
	bareMetalClusterResource string = "BareMetalCluster"
	amsActionCreate          string = "create"
	clustersPathRegex        string = "clusters/.+"
	capabilityName           string = "bare_metal_installer_admin"
	capabilityType           string = "Account"
)

// CreateAuthorizer returns Authorizer if auth is enabled
func (a *AuthHandler) CreateAuthorizer() func(*http.Request) error {
	if !a.EnableAuth {
		return func(*http.Request) error {
			return nil
		}
	}

	return a.Authorizer
}

// Authorizer is used to authorize a request after the Auth function was called using the "Auth*" functions
// and the principal was stored in the context in the "AuthKey" context value.
func (a *AuthHandler) Authorizer(request *http.Request) error {
	payload := PayloadFromContext(request.Context())
	if payload == nil || payload.Username == "" {
		// auth is disabled
		return nil
	}
	username := payload.Username
	payloadFromCache, found := a.client.Cache.Get(username)
	if found {
		// Update payload with values from cache
		payloadFromCache := payloadFromCache.(*ocm.AuthPayload)
		payload.IsUser = payloadFromCache.IsUser
		payload.IsAdmin = payloadFromCache.IsAdmin
	} else {
		allowed, err := a.allowedToUseAssistedInstaller(username)
		if err != nil {
			logrus.Errorf("Failed to authorize user: %v", err)
		}
		payload.IsUser = allowed
		a.client.Cache.Set(username, payload, cache.DefaultExpiration)
	}

	if payload.IsUser {
		if payload.IsAdmin {
			// All endpoints are allowed for admins.
			return nil
		}

		// If endpoint contains a cluster ID, ensures it's owned by the user.
		if clusterID := a.getClusterIDFromPath(request.URL.Path); clusterID != "" {
			if a.isClusterOwnedByUser(clusterID, username) {
				// User allowed to manipulate only owned cluster.
				return nil
			}
		} else {
			// Other API endpoints are allowed for all authorized users.
			return nil
		}
	}

	return fmt.Errorf("method is not allowed")
}

// Ensure that the user has authorization to use the bare metal installer service.
// For now the indication is simply "create BareMetalCluster" permission,
// which is allowed for users with BareMetalInstallerUser role.
func (a *AuthHandler) allowedToUseAssistedInstaller(username string) (bool, error) {
	return a.client.Authorization.AccessReview(
		context.Background(), username, amsActionCreate, bareMetalClusterResource)
}

// Extracts cluster ID for path if available
func (a *AuthHandler) getClusterIDFromPath(path string) string {
	re := regexp.MustCompile(clustersPathRegex)
	if re.MatchString(path) {
		res := strings.Split(re.FindString(path), "/")
		return res[1]
	}
	return ""
}

// Checks whether the cluster owned by the user
func (a *AuthHandler) isClusterOwnedByUser(clusterID, username string) bool {
	if err := a.db.First(&common.Cluster{}, "id = ? and user_name = ?", clusterID, username).Error; err == nil {
		// Cluster owned by the user
		return true
	}
	return false
}

// IsAdmin checks whether user is an admin
func (a *AuthHandler) isAdmin(username string) (bool, error) {
	return a.client.Authorization.CapabilityReview(
		context.Background(), fmt.Sprint(username), capabilityName, capabilityType)
}

// PayloadFromContext returns auth payload from the specified context
func PayloadFromContext(ctx context.Context) *ocm.AuthPayload {
	payload := ctx.Value(restapi.AuthKey)
	if payload == nil {
		return nil
	}
	return payload.(*ocm.AuthPayload)
}

// UserNameFromContext returns username from the specified context
func UserNameFromContext(ctx context.Context) string {
	payload := PayloadFromContext(ctx)
	if payload == nil {
		return ""
	}
	return payload.Username
}

// OrgIDFromContext returns org ID from the specified context
func OrgIDFromContext(ctx context.Context) string {
	payload := PayloadFromContext(ctx)
	if payload == nil {
		return ""
	}
	return payload.Organization
}
