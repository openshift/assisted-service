package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/security"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	params "github.com/openshift/assisted-service/pkg/context"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

type RHSSOAuthenticator struct {
	KeyMap     map[string]*rsa.PublicKey
	AdminUsers []string
	utils      AUtilsInteface
	log        logrus.FieldLogger
	client     *ocm.Client
	db         *gorm.DB
}

func NewRHSSOAuthenticator(cfg *Config, ocmCLient *ocm.Client, log logrus.FieldLogger, db *gorm.DB) *RHSSOAuthenticator {
	a := &RHSSOAuthenticator{
		AdminUsers: cfg.AdminUsers,
		utils:      NewAuthUtils(cfg.JwkCert, cfg.JwkCertURL),
		client:     ocmCLient,
		log:        log,
		db:         db,
	}
	err := a.populateKeyMap()
	if err != nil {
		log.Fatalln("Failed to init auth handler,", err)
	}
	return a
}

var _ Authenticator = &RHSSOAuthenticator{}

func (a *RHSSOAuthenticator) AuthType() AuthType {
	return TypeRHSSO
}

func (a *RHSSOAuthenticator) populateKeyMap() error {
	// Load the trusted CA certificates:
	trustedCAs, err := x509.SystemCertPool()
	if err != nil {
		return errors.Errorf("can't load system trusted CAs: %v", err)
	}

	// Try to read the JWT public key object file.
	a.KeyMap, err = a.utils.proccessPublicKeys(trustedCAs)
	return err
}

func (a *RHSSOAuthenticator) getValidationToken(token *jwt.Token) (interface{}, error) {
	// Try to get the token kid.
	kid, ok := token.Header["kid"]
	if !ok {
		return nil, errors.Errorf("no kid found in jwt token")
	}

	// Try to get correct cert from certs map.
	key, ok := a.KeyMap[kid.(string)]
	if !ok {
		return nil, errors.Errorf("No matching key in auth keymap for key id [%v]", kid)
	}

	return key, nil
}

func (a *RHSSOAuthenticator) AuthAgentAuth(token string) (interface{}, error) {
	if a.client == nil {
		a.log.Error("OCM client unavailable")
		return nil, errors.Errorf("OCM client unavailable")
	}
	authUser, found := a.client.Cache.Get(token)
	if found {
		return authUser, nil
	}
	user, err := a.client.Authentication.AuthenticatePullSecret(context.Background(), token)
	if err != nil {
		a.log.Errorf("Error Authenticating PullSecret token: %v", err)
		return nil, common.ApiErrorWithDefaultInfraError(err, http.StatusUnauthorized)
	}
	err = a.storeRoleInPayload(user)

	if shouldStorePayloadInCache(err) {
		a.client.Cache.Set(token, user, cache.DefaultExpiration)
	}

	if err != nil {
		a.log.Errorf("Unable to fetch user's capabilities: %v", err)
		return nil, common.ApiErrorWithDefaultInfraError(err, http.StatusUnauthorized)
	}
	return user, nil
}

func parseOCMPayload(userToken *jwt.Token) (*ocm.AuthPayload, error) {
	claims, ok := userToken.Claims.(jwt.MapClaims)
	if !ok {
		err := errors.Errorf("Unable to parse JWT token claims")
		return nil, err
	}

	payload := &ocm.AuthPayload{}
	// default to the values we expect from RHSSO
	payload.Username, _ = claims["username"].(string)
	payload.FirstName, _ = claims["first_name"].(string)
	payload.LastName, _ = claims["last_name"].(string)
	payload.Organization, _ = claims["org_id"].(string)
	payload.Email, _ = claims["email"].(string)
	payload.ClientID, _ = claims["clientId"].(string)

	// Check values, if empty, use alternative claims from RHD
	if payload.Username == "" {
		payload.Username, _ = claims["preferred_username"].(string)
	}

	if payload.FirstName == "" {
		payload.FirstName, _ = claims["given_name"].(string)
	}

	if payload.LastName == "" {
		payload.LastName, _ = claims["family_name"].(string)
	}

	// If given and family names are not present, use the name field
	if payload.FirstName == "" || payload.LastName == "" {
		name, _ := claims["name"].(string)
		names := strings.Split(name, " ")
		if len(names) > 1 {
			payload.FirstName = names[0]
			payload.LastName = names[1]
		} else {
			payload.FirstName = names[0]
		}
	}
	return payload, nil
}

func isValidationErrorIssuedAt(err error) bool {
	e, ok := err.(*jwt.ValidationError)
	if !ok {
		return false
	}
	return e.Errors == jwt.ValidationErrorIssuedAt
}

func (a *RHSSOAuthenticator) AuthUserAuth(token string) (interface{}, error) {
	// Handle Bearer
	authHeaderParts := strings.Fields(token)
	if len(authHeaderParts) != 2 || strings.ToLower(authHeaderParts[0]) != "bearer" {
		return nil, errors.Errorf("Authorization header format must be Bearer {token}")
	}
	// Now parse the token
	parsedToken, err := jwt.Parse(authHeaderParts[1], a.getValidationToken)

	// Check if there was an error in parsing and if the parsed token is valid
	if err != nil || !parsedToken.Valid {
		// Don't report error "Token used before issued"
		// TODO: This validation is going to be removed in jwt-go v4, once jwt-go v4
		// is released and we start using it, this validation-skip can be removed.
		if !isValidationErrorIssuedAt(err) {
			a.log.WithError(err).Errorf("Error parsing token or token is invalid")
			return nil, errors.Errorf("Error parsing token or token is invalid")
		}
	}

	if jwt.SigningMethodRS256 != nil && jwt.SigningMethodRS256.Alg() != parsedToken.Header["alg"] {
		message := fmt.Sprintf("Expected %s signing method but token specified %s",
			jwt.SigningMethodRS256.Alg(),
			parsedToken.Header["alg"])
		a.log.Errorf("Error validating token algorithm: %s", message)
		return nil, errors.Errorf("Error validating token algorithm: %s", message)
	}

	payload, err := parseOCMPayload(parsedToken)
	if err != nil {
		a.log.Error("Failed parse payload,", err)
		return nil, err
	}

	if payload.Username == "" {
		a.log.Error("Missing username in token")
		return nil, errors.Errorf("Missing username in token")
	}

	payloadKey := payload.Username + "_is_admin"
	if payloadFromCache, existInCache := a.client.Cache.Get(payloadKey); existInCache {
		payload.Role = payloadFromCache.(*ocm.AuthPayload).Role
	} else {
		err := a.storeRoleInPayload(payload)

		if shouldStorePayloadInCache(err) {
			a.client.Cache.Set(payloadKey, payload, cache.DefaultExpiration)
		}

		if err != nil {
			a.log.Errorf("Unable to fetch user's role: %v", err)
			return nil, common.ApiErrorWithDefaultInfraError(err, http.StatusUnauthorized)
		}
	}
	return payload, nil
}

func (a RHSSOAuthenticator) storeRoleInPayload(payload *ocm.AuthPayload) error {
	role, err := a.getRole(payload)
	if err != nil {
		return err
	}
	payload.Role = role
	return nil
}

func (a RHSSOAuthenticator) getRole(payload *ocm.AuthPayload) (ocm.RoleType, error) {
	if funk.Contains(a.AdminUsers, payload.Username) {
		return ocm.AdminRole, nil
	}
	isReadOnly, err := a.isReadOnlyAdmin(payload.Username)
	if err != nil {
		return ocm.UserRole, err
	}
	if isReadOnly {
		return ocm.ReadOnlyAdminRole, nil
	}
	return ocm.UserRole, nil
}

func (a *RHSSOAuthenticator) isReadOnlyAdmin(username string) (bool, error) {
	cacheKey := fmt.Sprintf("%s-%s-%s", username, ocm.CapabilityName, ocm.CapabilityType)
	if cacheData, existInCache := a.client.Cache.Get(cacheKey); existInCache {
		isAllowed, ok := cacheData.(bool)
		if !ok {
			return false, errors.New("Expected from cache data to be from bool kind")
		}
		return isAllowed, nil
	}

	isAllowed, err := a.client.Authorization.CapabilityReview(context.Background(), fmt.Sprint(username), ocm.CapabilityName, ocm.CapabilityType)
	if shouldStorePayloadInCache(err) {
		a.client.Cache.Set(cacheKey, isAllowed, 1*time.Minute)
	}
	return isAllowed, err
}

func (a *RHSSOAuthenticator) isClusterOwnedByUser(clusterID string, payload *ocm.AuthPayload) (bool, error) {
	roll, _ := a.getRole(payload)
	if roll != ocm.UserRole {
		return true, nil //admins has always access to the cluster
	}

	if clusterID == "" {
		return true, nil //not an API of clusters, so grant permission to access
	}

	if a.db != nil {
		err := a.db.First(&common.Cluster{}, "id = ? and user_name = ?", clusterID, payload.Username).Error
		if err != nil {
			//if user is not the owner of the cluster return false
			if err == gorm.ErrRecordNotFound {
				return false, nil
			}
			//in case of a real db error, indicate it to the caller
			return false, err
		}
	}

	return true, nil
}

func (a *RHSSOAuthenticator) AuthURLAuth(_ string) (interface{}, error) {
	return nil, errors.Errorf("URL Authentication not allowed for rhsso auth")
}

func (a *RHSSOAuthenticator) CreateAuthenticator() func(name, in string, authenticate security.TokenAuthentication) runtime.Authenticator {
	return func(name string, _ string, authenticate security.TokenAuthentication) runtime.Authenticator {
		getToken := func(r *http.Request) string { return r.Header.Get(name) }

		return security.HttpAuthenticator(func(r *http.Request) (bool, interface{}, error) {
			log := logutil.FromContext(r.Context(), a.log)
			token := getToken(r)
			if token == "" {
				return false, nil, nil
			}
			p, err := authenticate(token)
			if err != nil {
				log.Errorf("Fail to authenticate. Error %v", err)
				if common.IsKnownError(err) {
					return true, nil, err
				}
				return true, nil, common.NewInfraError(http.StatusUnauthorized, err)
			}
			//this code is part of the authorization process and should move to authz_handler
			//after https://github.com/go-openapi/runtime/issues/158 is resolved
			clusterID := params.GetParam(r.Context(), params.ClusterId)
			ownedBy, err := a.isClusterOwnedByUser(clusterID, p.(*ocm.AuthPayload))
			if err != nil {
				log.Errorf("Fail to verify access to cluster. Error %v", err)
				return true, nil, common.NewApiError(http.StatusInternalServerError, err)
			}
			if !ownedBy {
				return true, nil, common.NewApiError(http.StatusNotFound, errors.New("Cluster Not Found"))
			}

			return true, p, nil
		})
	}
}
