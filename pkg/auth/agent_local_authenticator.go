package auth

import (
	"crypto"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/security"
	"github.com/golang-jwt/jwt/v4"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type AgentLocalAuthenticator struct {
	log       logrus.FieldLogger
	publicKey crypto.PublicKey
}

func NewAgentLocalAuthenticator(cfg *Config, log logrus.FieldLogger) (*AgentLocalAuthenticator, error) {
	if cfg.ECPublicKeyPEM == "" {
		return nil, errors.Errorf("agent installer local authentication requires an ecdsa Public Key")
	}

	// When generating an agent ISO, the Agent installer creates ECDSA public/private keys.
	// However, the systemd services of the Agent installer fail to parse multiline keys accurately.
	// To address this, the keys are encoded in base64 format to condense them into a single line
	// before being transmitted to the assisted service.
	// Upon reception, the assisted service decodes these keys back to their original multiline format
	// for subsequent processing.

	decodedECPublicKeyPEM, err := base64.StdEncoding.DecodeString(cfg.ECPublicKeyPEM)
	if err != nil {
		log.WithError(err).Fatal("Error decoding public key:")
	}
	cfg.ECPublicKeyPEM = string(decodedECPublicKeyPEM)

	key, err := jwt.ParseECPublicKeyFromPEM([]byte(cfg.ECPublicKeyPEM))
	if err != nil {
		return nil, err
	}
	a := &AgentLocalAuthenticator{
		log:       log,
		publicKey: key,
	}

	return a, nil
}

var _ Authenticator = &AgentLocalAuthenticator{}

func (a *AgentLocalAuthenticator) AuthType() AuthType {
	return TypeAgentLocal
}

func (a *AgentLocalAuthenticator) EnableOrgTenancy() bool {
	return false
}

func (a *AgentLocalAuthenticator) EnableOrgBasedFeatureGates() bool {
	return false
}

func (a *AgentLocalAuthenticator) authenticateToken(token string) (interface{}, error) {
	t, err := validateToken(token, a.publicKey)
	if err != nil {
		a.log.WithError(err).Error("failed to validate token")
		return nil, common.NewInfraError(http.StatusUnauthorized, err)
	}
	claims, ok := t.Claims.(jwt.MapClaims)
	if !ok {
		err = errors.Errorf("failed to parse JWT token claims")
		a.log.Error(err)
		return nil, common.NewInfraError(http.StatusUnauthorized, err)
	}

	exp, found := claims["exp"].(float64)
	if !found {
		// exp claim is not found in the case of install workflow
		return claims, nil
	}
	// in the case of addnodes workflow, check if the token is expired
	expTime := time.Unix(int64(exp), 0)
	if expTime.Before(time.Now().UTC()) {
		err = errors.Errorf("The provided authentication token has expired. Please generate a new token and try again.")
		a.log.Error(err)
		return nil, common.NewInfraError(http.StatusUnauthorized, err)
	}
	return claims, nil
}

func (a *AgentLocalAuthenticator) AuthAgentAuth(token string) (interface{}, error) {
	return a.authenticateToken(token)
}

func (a *AgentLocalAuthenticator) AuthUserAuth(token string) (interface{}, error) {
	return a.authenticateToken(token)
}

func (a *AgentLocalAuthenticator) AuthURLAuth(token string) (interface{}, error) {
	return nil, common.NewInfraError(http.StatusUnauthorized, errors.Errorf("URL Authentication not allowed for agent local auth"))
}

func (a *AgentLocalAuthenticator) AuthImageAuth(_ string) (interface{}, error) {
	return nil, common.NewInfraError(http.StatusUnauthorized, errors.Errorf("Image Authentication not allowed for agent local auth"))
}

func (a *AgentLocalAuthenticator) AuthWatcherAuth(token string) (interface{}, error) {
	return a.authenticateToken(token)
}

func (a *AgentLocalAuthenticator) CreateAuthenticator() func(_, _ string, _ security.TokenAuthentication) runtime.Authenticator {
	return security.APIKeyAuth
}
