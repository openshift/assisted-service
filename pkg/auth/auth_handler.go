package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"

	"github.com/openshift/assisted-service/pkg/ocm"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/security"

	"github.com/dgrijalva/jwt-go"
	"github.com/sirupsen/logrus"
)

type Config struct {
	EnableAuth      bool   `envconfig:"ENABLE_AUTH" default:"false"`
	EnableAuthAgent bool   `envconfig:"ENABLE_AUTH_AGENT" default:"true"`
	EnableAuthz     bool   `envconfig:"ENABLE_AUTH_AGENT" default:"true"`
	JwkCert         string `envconfig:"JWKS_CERT"`
	JwkCertURL      string `envconfig:"JWKS_URL" default:"https://api.openshift.com/.well-known/jwks.json"`
	// Will be split with "," as separator
	AllowedDomains string `envconfig:"ALLOWED_DOMAINS" default:""`
}

type AuthHandler struct {
	EnableAuth      bool
	EnableAuthAgent bool
	EnableAuthz     bool
	KeyMap          map[string]*rsa.PublicKey
	utils           AUtilsInteface
	log             logrus.FieldLogger
	client          *ocm.Client
}

func NewAuthHandler(cfg Config, ocmCLient *ocm.Client, log logrus.FieldLogger) *AuthHandler {
	a := &AuthHandler{
		EnableAuth:      cfg.EnableAuth,
		EnableAuthz:     cfg.EnableAuthz,
		EnableAuthAgent: cfg.EnableAuthAgent,
		utils:           NewAuthUtils(cfg.JwkCert, cfg.JwkCertURL),
		client:          ocmCLient,
		log:             log,
	}
	if a.EnableAuth {
		err := a.populateKeyMap()
		if err != nil {
			log.Fatalln("Failed to init auth handler,", err)
		}
	}
	return a
}

func (a *AuthHandler) populateKeyMap() error {
	// Load the trusted CA certificates:
	trustedCAs, err := x509.SystemCertPool()
	if err != nil {
		return fmt.Errorf("can't load system trusted CAs: %v", err)
	}

	// Try to read the JWT public key object file.
	a.KeyMap, err = a.utils.proccessPublicKeys(trustedCAs)
	return err
}

func (a *AuthHandler) getValidationToken(token *jwt.Token) (interface{}, error) {
	// Try to get the token kid.
	kid, ok := token.Header["kid"]
	if !ok {
		return nil, fmt.Errorf("no kid found in jwt token")
	}

	// Try to get correct cert from certs map.
	key, ok := a.KeyMap[kid.(string)]
	if !ok {
		return nil, fmt.Errorf("No matching key in auth keymap for key id [%v]", kid)
	}

	return key, nil
}

func (a *AuthHandler) AuthAgentAuth(token string) (interface{}, error) {
	if !a.EnableAuthAgent {
		// return a fake user for subsystem
		return &ocm.AuthPayload{Username: AdminUsername, IsAdmin: true}, nil
	}
	if a.client == nil {
		a.log.Error("OCM client unavailable")
		return nil, fmt.Errorf("OCM client unavailable")
	}
	user, err := a.client.Authentication.AuthenticatePullSecret(context.Background(), token)
	if err != nil {
		a.log.Error("Error Authenticating PullSecret token: %v", err)
		return nil, err
	}
	err = a.storeAdminInPayload(user)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func parsePayload(userToken *jwt.Token) (*ocm.AuthPayload, error) {
	claims, ok := userToken.Claims.(jwt.MapClaims)
	if !ok {
		err := fmt.Errorf("Unable to parse JWT token claims")
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

func (a *AuthHandler) AuthUserAuth(token string) (interface{}, error) {
	// Handle Bearer
	authHeaderParts := strings.Fields(token)
	if len(authHeaderParts) != 2 || strings.ToLower(authHeaderParts[0]) != "bearer" {
		return nil, fmt.Errorf("Authorization header format must be Bearer {token}")
	}
	// Now parse the token
	parsedToken, err := jwt.Parse(authHeaderParts[1], a.getValidationToken)

	// Check if there was an error in parsing...
	if err != nil {
		a.log.Errorf("Error parsing token: %e", err)
		return nil, fmt.Errorf("Error parsing token: %v", err)
	}

	if jwt.SigningMethodRS256 != nil && jwt.SigningMethodRS256.Alg() != parsedToken.Header["alg"] {
		message := fmt.Sprintf("Expected %s signing method but token specified %s",
			jwt.SigningMethodRS256.Alg(),
			parsedToken.Header["alg"])
		a.log.Errorf("Error validating token algorithm: %s", message)
		return nil, fmt.Errorf("Error validating token algorithm: %s", message)
	}

	// Check if the parsed token is valid...
	if !parsedToken.Valid {
		a.log.Error("Token is invalid: %s", parsedToken.Raw)
		return nil, fmt.Errorf("Token is invalid: %s", parsedToken.Raw)
	}

	payload, err := parsePayload(parsedToken)
	if err != nil {
		a.log.Error("Failed parse payload,", err)
		return nil, err
	}

	err = a.storeAdminInPayload(payload)
	if err != nil {
		return nil, err
	}

	if payload.Username == "" {
		a.log.Error("Missing username in token")
		return nil, fmt.Errorf("Missing username in token")
	}

	return payload, nil
}

func (a *AuthHandler) storeAdminInPayload(payload *ocm.AuthPayload) error {
	if !a.EnableAuthz {
		// a fake admin user for subsystem
		payload.IsAdmin = true
		return nil
	}
	admin, err := a.isAdmin(payload.Username)
	if err != nil {
		return fmt.Errorf("Unable to fetch user's capabilities: %v", err)
	}
	payload.IsAdmin = admin
	return nil
}

func (a *AuthHandler) isAdmin(username string) (bool, error) {
	return a.client.Authorization.CapabilityReview(
		context.Background(), fmt.Sprint(username), capabilityName, capabilityType)
}

func (a *AuthHandler) CreateAuthenticator() func(name, in string, authenticate security.TokenAuthentication) runtime.Authenticator {
	return func(name string, _ string, authenticate security.TokenAuthentication) runtime.Authenticator {
		getToken := func(r *http.Request) string { return r.Header.Get(name) }

		return security.HttpAuthenticator(func(r *http.Request) (bool, interface{}, error) {
			if !a.EnableAuth {
				a.log.Debug("API Key Authentication Disabled")
				return true, &ocm.AuthPayload{
					IsAdmin:  true, // auth disabled - behave as system-admin
					Username: AdminUsername,
				}, nil
			}
			token := getToken(r)
			if token == "" {
				return false, nil, nil
			}
			p, err := authenticate(token)
			if err != nil {
				return false, nil, err
			}
			return true, p, nil
		})
	}
}
