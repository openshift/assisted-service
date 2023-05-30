package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/golang-jwt/jwt/v4"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	params "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

/* AuthzHandler is the authorizer middleware that is being used for
 * RHSSO authentication cases and work with AMS to ensure tenancy
 * and user based access policies
 */
type AuthzHandler struct {
	cfg    *Config
	log    logrus.FieldLogger
	client *ocm.Client
	db     *gorm.DB
}

func (a *AuthzHandler) CreateAuthorizer() func(*http.Request) error {
	return a.authorizerMiddleware
}

func (a *AuthzHandler) isTenancyEnabled() bool {
	return a.cfg.EnableOrgTenancy
}

func (a *AuthzHandler) isOrgBasedFunctionalityEnabled() bool {
	return a.cfg.EnableOrgBasedFeatureGates
}

func (a *AuthzHandler) IsAdmin(ctx context.Context) bool {
	authPayload := ocm.PayloadFromContext(ctx)
	allowedRoles := []ocm.RoleType{ocm.AdminRole, ocm.ReadOnlyAdminRole}
	return funk.Contains(allowedRoles, authPayload.Role)
}

func (a *AuthzHandler) isReadOnlyAdmin(ctx context.Context) bool {
	authPayload := ocm.PayloadFromContext(ctx)
	allowedRoles := []ocm.RoleType{ocm.ReadOnlyAdminRole}
	return funk.Contains(allowedRoles, authPayload.Role)
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

func (a *AuthzHandler) OwnedBy(ctx context.Context, db *gorm.DB) *gorm.DB {
	if a.IsAdmin(ctx) {
		return db
	}
	if a.isTenancyEnabled() {
		return db.Where("org_id = ?", ocm.OrgIDFromContext(ctx))
	} else {
		return db.Where("user_name = ?", ocm.UserNameFromContext(ctx))
	}
}

func (a *AuthzHandler) OwnedByUser(ctx context.Context, db *gorm.DB, username string) *gorm.DB {
	// When tenancy-based access is supported, the following query returns records
	// for the input user alone. With a user-based access policy, the query returns
	// the user's records, provided it is the current user. Otherwise, it returns an
	// empty set since we do not support listing objects on behalf of other users in
	// that mode.
	if username == "" {
		return a.OwnedBy(ctx, db)
	}
	return a.OwnedBy(ctx, db).Where("user_name = ?", username)
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
		err = a.db.Select("ams_subscription_id", "openshift_cluster_id", "kind").
			First(&cluster, "id = ?", clusterId).Error
		if err != nil {
			return handleOwnershipQueryError(err)
		}

		// Handle day2 cluster access by finding the 'ams_subscription_id' of the day1 cluster.
		if *cluster.Kind == models.ClusterKindAddHostsCluster {
			err = a.db.Select("ams_subscription_id").
				First(&cluster, "openshift_cluster_id = ? and kind = ?", cluster.OpenshiftClusterID, models.ClusterKindCluster).Error
			if err != nil {
				return handleOwnershipQueryError(err)
			}
		}

		isAllowed, err = a.hasClusterEditRole(payload, action, cluster.AmsSubscriptionID.String())
		return isAllowed, err
	}

	return false, err
}

func (a *AuthzHandler) HasAccessTo(ctx context.Context, obj interface{}, action Action) (bool, error) {
	if a.isReadOnlyAdmin(ctx) {
		if action == ReadAction {
			return true, nil
		}
	} else if a.IsAdmin(ctx) {
		return true, nil
	}
	if cluster, ok := obj.(*common.Cluster); ok && cluster != nil {
		return a.checkClusterBasedAccess(cluster.ID.String(), action, ocm.PayloadFromContext(ctx))
	}
	if infraEnv, ok := obj.(*common.InfraEnv); ok && infraEnv != nil {
		return a.checkInfraEnvBasedAccess(infraEnv.ID.String(), action, ocm.PayloadFromContext(ctx))
	}
	if host, ok := obj.(*common.Host); ok && host != nil {
		if host.ClusterID != nil {
			return a.checkClusterBasedAccess(host.ClusterID.String(), action, ocm.PayloadFromContext(ctx))
		}
		return a.checkInfraEnvBasedAccess(host.InfraEnvID.String(), action, ocm.PayloadFromContext(ctx))
	}
	return false, errors.New("can not perform access check on this object")
}

func (a *AuthzHandler) HasOrgBasedCapability(ctx context.Context, capability string) (bool, error) {
	if !a.isOrgBasedFunctionalityEnabled() {
		return true, nil
	}

	username := ocm.UserNameFromContext(ctx)
	isAllowed, err := a.client.Authorization.CapabilityReview(context.Background(), fmt.Sprint(username), capability, ocm.OrganizationCapabilityType)
	a.log.Debugf("queried AMS API with CapabilityReview for username: %s about capability: %s, capability type: %s. Result: %t",
		fmt.Sprint(username), capability, ocm.OrganizationCapabilityType, isAllowed)

	if err != nil {
		a.log.WithError(err).Errorf("error getting user %s capability", capability)
	}

	return isAllowed, err
}

func (a *AuthzHandler) checkClusterBasedAccess(id string, action Action, payload *ocm.AuthPayload) (bool, error) {
	if a.db == nil {
		return true, nil
	}

	switch action {
	case UpdateAction:
		return a.hasSubscriptionAccess(id, ocm.AMSActionUpdate, payload)
	case DeleteAction:
		return a.hasSubscriptionAccess(id, ocm.AMSActionUpdate, payload)
	default:
		return a.hasOwnerAccess(id, &common.Cluster{}, payload)
	}
}

func (a *AuthzHandler) checkInfraEnvBasedAccess(id string, action Action, payload *ocm.AuthPayload) (bool, error) {
	if a.db == nil {
		return true, nil
	}

	if !a.isTenancyEnabled() {
		return a.isObjectOwnedByUser(id, &common.InfraEnv{}, payload)
	}

	var isAllowed bool
	var err error

	//if the infraenv is bound to a cluster the access check
	//are performed based on the bound cluster data. As a fallback
	//we test for ownership on the infraenv object itself
	var infraEnv common.InfraEnv
	err = a.db.Select("cluster_id").First(&infraEnv, "id = ?", id).Error
	if err != nil {
		a.log.WithError(err).Errorf("failed to retrieve infra-env record %s", id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// not returning err as the response should be StatusNotFound
			return false, nil
		}
		return false, err
	}

	// read action is always available for owners
	if action == ReadAction {
		return a.hasOwnerAccess(id, &common.InfraEnv{}, payload)
	}

	// write actions are always available for the strict owner
	// (the user that created this object)
	if isAllowed, err = a.isObjectOwnedByUser(id, &common.InfraEnv{}, payload); isAllowed {
		return true, nil
	}

	//otherwise, update actions has 2 cases:
	//for bound infra-env, check the access rights on the cluster
	if infraEnv.ClusterID != "" {
		if isAllowed, err = a.checkClusterBasedAccess(infraEnv.ClusterID.String(), action, payload); isAllowed {
			return true, nil
		}
	}
	return false, err
}

func (a *AuthzHandler) authorizerMiddleware(request *http.Request) error {
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
			isAllowed, err = a.checkClusterBasedAccess(clusterID, toAction(request), payload)
		} else if infraEnvID := params.GetParam(request.Context(), params.InfraEnvId); infraEnvID != "" {
			isAllowed, err = a.checkInfraEnvBasedAccess(infraEnvID, toAction(request), payload)
		}
		if err != nil {
			a.log.Errorf("Failed to verify access to object. Error %v", err)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
		if !isAllowed {
			obj := a.getObjFromRequest(request)
			if obj != nil && toAction(request) != http.MethodGet {
				// Check if user has read access (needed for returning an appropriate http status)
				// Returns status forbidden if only read is allowed on object
				if canRead, _ := a.HasAccessTo(request.Context(), obj, ReadAction); canRead {
					return common.NewInfraError(http.StatusForbidden, fmt.Errorf("Unauthorized to manipulate object"))
				}
			}
			return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
		}
	}

	return nil
}

func (a *AuthzHandler) getObjFromRequest(request *http.Request) interface{} {
	if clusterID := params.GetParam(request.Context(), params.ClusterId); clusterID != "" {
		id := strfmt.UUID(clusterID)
		cluster := &common.Cluster{Cluster: models.Cluster{ID: &id}}
		return cluster
	} else if infraEnvID := params.GetParam(request.Context(), params.InfraEnvId); infraEnvID != "" {
		id := strfmt.UUID(infraEnvID)
		infraEnv := &common.InfraEnv{InfraEnv: models.InfraEnv{ID: &id}}
		return infraEnv
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
