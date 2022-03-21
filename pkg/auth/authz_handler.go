package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-openapi/runtime/middleware"
	"github.com/golang-jwt/jwt/v4"
	"github.com/openshift/assisted-service/internal/common"
	params "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

type AuthzHandler struct {
	cfg     *Config
	Enabled bool
	log     logrus.FieldLogger
	client  *ocm.Client
	db      *gorm.DB
}

func NewAuthzHandler(cfg *Config, ocmCLient *ocm.Client, log logrus.FieldLogger, db *gorm.DB) *AuthzHandler {
	a := &AuthzHandler{
		cfg:     cfg,
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

func (a *AuthzHandler) isTenancyEnabled() bool {
	return a.cfg.EnableOrgTenancy
}

func handleOwnershipQueryError(err error) (bool, error) {
	if err != nil {
		//if user is not the owner of the object return false
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		//in case of a real db error, indicate it to the caller
		return false, err
	}
	return true, nil
}

func (a *AuthzHandler) isObjectOwnedByUser(id string, obj interface{}, payload *ocm.AuthPayload) (bool, error) {
	if a.db != nil {
		err := a.db.First(obj, "id = ? and user_name = ?", id, payload.Username).Error
		return handleOwnershipQueryError(err)
	}

	return true, nil
}

func (a *AuthzHandler) isObjectOwnedByOrg(id string, obj interface{}, payload *ocm.AuthPayload) (bool, error) {
	if a.db != nil {
		err := a.db.First(obj, "id = ? and org_id = ?", id, payload.Organization).Error
		return handleOwnershipQueryError(err)
	}

	return true, nil
}

func (a *AuthzHandler) hasOwnerAccess(id string, obj interface{}, payload *ocm.AuthPayload) (ownedBy bool, err error) {
	//MGMT-9443 TODO: cache this
	//If the object is owned by the requested user, always approve the access
	if ownedBy, err = a.isObjectOwnedByUser(id, obj, payload); ownedBy {
		return true, nil
	}

	//If the object is not owned by the user and tenancy access is enabled
	//check if the user belongs to the same organization as the resource owner
	if err == nil && a.isTenancyEnabled() {
		return a.isObjectOwnedByOrg(id, obj, payload)
	}

	//in case error occured before, return it
	return
}

func (a *AuthzHandler) hasSubscriptionAccess(clusterId string, action string, payload *ocm.AuthPayload) (isAllowed bool, err error) {
	if isAllowed, err = a.isObjectOwnedByUser(clusterId, &common.Cluster{}, payload); isAllowed {
		return true, nil
	}

	if a.isTenancyEnabled() {
		var cluster common.Cluster
		err = a.db.Select("ams_subscription_id").First(&cluster, "id = ?", clusterId).Error
		if err != nil {
			return handleOwnershipQueryError(err)
		}

		cacheKey := fmt.Sprintf("%s_%s_%s", payload.Username, payload.Organization, cluster.AmsSubscriptionID)
		if cacheData, existInCache := a.client.Cache.Get(cacheKey); existInCache {
			var ok bool
			isAllowed, ok = cacheData.(bool)
			if !ok {
				return false, fmt.Errorf(
					"error while retrieving cluster edit role from cache for %s",
					cluster.AmsSubscriptionID.String())
			}
			return isAllowed, nil
		}

		isAllowed, err = a.hasClusterEditRole(payload, action, cluster.AmsSubscriptionID.String())
		if shouldStorePayloadInCache(err) {
			a.client.Cache.Set(cacheKey, isAllowed, 10*time.Minute)
		}
		return isAllowed, err
	}

	return false, err
}

func (a *AuthzHandler) checkClusterBasedAccess(id string, request *http.Request, payload *ocm.AuthPayload) (bool, error) {
	if a.db == nil {
		return true, nil
	}

	switch {
	case isUpdateRequest(request):
		return a.hasSubscriptionAccess(id, ocm.AMSActionUpdate, payload)
	case isDeleteRequest(request):
		return a.hasSubscriptionAccess(id, ocm.AMSActionDelete, payload)
	default:
		return a.hasOwnerAccess(id, &common.Cluster{}, payload)
	}
}

func (a *AuthzHandler) checkInfraEnvBasedAccess(id string, request *http.Request, payload *ocm.AuthPayload) (bool, error) {
	if a.db == nil {
		return true, nil
	}

	if !a.isTenancyEnabled() {
		return a.isObjectOwnedByUser(id, &common.InfraEnv{}, payload)
	}

	//if the infraenv is bound to a cluster the access check
	//are performed based on the bound cluster data. As a fallback
	//we test for ownership on the infraenv object itself
	var infraEnv common.InfraEnv
	err := a.db.Select("cluster_id").First(&infraEnv, "id = ?", id).Error
	if err != nil {
		a.log.WithError(err).Errorf("failed to retrieve infra-env record %s", id)
		return false, err
	}

	if infraEnv.ClusterID != "" {
		if isAllowed, _ := a.checkClusterBasedAccess(infraEnv.ClusterID.String(), request, payload); isAllowed {
			return true, nil
		}
	}

	//fallback option for both failures in bound infraEnv access checks and
	//for non bound infraEnv related objects
	return a.isObjectOwnedByUser(id, &common.InfraEnv{}, payload)
}

func (a *AuthzHandler) Authorizer(request *http.Request) error {
	route := middleware.MatchedRouteFrom(request)
	switch authScheme := route.Authenticator.Schemes[0]; authScheme {
	case "imageAuth", "imageURLAuth":
		return a.imageTokenAuthorizer(request.Context())
	default:
		return a.ocmAuthorizer(request)
	}
}

func (a *AuthzHandler) imageTokenAuthorizer(ctx context.Context) error {
	payload := ctx.Value(restapi.AuthKey)
	if payload == nil {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("payload missing from authenticated context"))
	}

	claims, ok := payload.(jwt.MapClaims)
	if !ok {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("malformed claims payload"))
	}

	claimID, ok := claims["sub"].(string)
	if !ok {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("malformed sub claim"))
	}

	requestID := params.GetParam(ctx, params.InfraEnvId)
	if claimID == "" || requestID == "" {
		return common.NewApiError(http.StatusBadRequest, fmt.Errorf("infraEnv ID missing from claim or request"))
	}
	if claimID != requestID {
		return common.NewInfraError(http.StatusForbidden, fmt.Errorf("Token for infraEnv ID %s, is unauthorized to access infraEnv ID %s", claimID, requestID))
	}

	return nil
}

// ocmAuthorizer is used to authorize a request after the Auth function was called using the "Auth*" functions
// and the principal was stored in the context in the "AuthKey" context value.
func (a *AuthzHandler) ocmAuthorizer(request *http.Request) error {
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
			http.StatusForbidden,
			fmt.Errorf(
				"%s: Unauthorized to access route (access review failed)",
				username))
	}

	if payload.Role == ocm.UserRole {
		//List requests and resources outside the scope of clusters or infraEnvs
		//handle their authorization at the application level
		isAllowed := true
		if clusterID := params.GetParam(request.Context(), params.ClusterId); clusterID != "" {
			isAllowed, err = a.checkClusterBasedAccess(clusterID, request, payload)
		} else if infraEnvID := params.GetParam(request.Context(), params.InfraEnvId); infraEnvID != "" {
			isAllowed, err = a.checkInfraEnvBasedAccess(infraEnvID, request, payload)
		}
		if err != nil {
			a.log.Errorf("Failed to verify access to object. Error %v", err)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
		if !isAllowed {
			return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
		}
	}

	return nil
}

func (a *AuthzHandler) allowedToUseAssistedInstaller(username string) (bool, error) {
	return a.client.Authorization.AccessReview(
		context.Background(), username, ocm.AMSActionCreate, "", ocm.BareMetalClusterResource)
}

func (a *AuthzHandler) hasClusterEditRole(payload *ocm.AuthPayload, action, subscriptionID string) (bool, error) {
	return a.client.Authorization.AccessReview(
		context.Background(), payload.Username, action, subscriptionID, ocm.Subscription)
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
