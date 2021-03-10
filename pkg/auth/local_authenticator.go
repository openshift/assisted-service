package auth

import (
	"crypto"
	"net/http"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/security"
	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type LocalAuthenticator struct {
	log       logrus.FieldLogger
	publicKey crypto.PublicKey
	db        *gorm.DB
}

func NewLocalAuthenticator(cfg *Config, log logrus.FieldLogger, db *gorm.DB) (*LocalAuthenticator, error) {
	if cfg.ECPublicKeyPEM == "" {
		return nil, errors.Errorf("local authentication requires an ecdsa Public Key")
	}

	key, err := jwt.ParseECPublicKeyFromPEM([]byte(cfg.ECPublicKeyPEM))
	if err != nil {
		return nil, err
	}

	a := &LocalAuthenticator{
		log:       log,
		publicKey: key,
		db:        db,
	}

	return a, nil
}

var _ Authenticator = &LocalAuthenticator{}

func (a *LocalAuthenticator) AuthType() AuthType {
	return TypeLocal
}

func (a *LocalAuthenticator) AuthAgentAuth(token string) (interface{}, error) {
	t, err := validateToken(token, a.publicKey)
	if err != nil {
		a.log.WithError(err).Error("failed to validate token")
		return nil, err
	}
	claims, ok := t.Claims.(jwt.MapClaims)
	if !ok {
		err := errors.Errorf("failed to parse JWT token claims")
		a.log.Error(err)
		return nil, err
	}

	clusterID, ok := claims["cluster_id"].(string)
	if !ok {
		err := errors.Errorf("claims are incorrectly formatted")
		a.log.Error(err)
		return nil, err
	}

	if !cluster.ClusterExists(a.db, strfmt.UUID(clusterID)) {
		err := errors.Errorf("cluster %s does not exist", clusterID)
		a.log.Error(err)
		return nil, err
	}

	a.log.Debugf("Authenticating cluster %s JWT", clusterID)
	return ocm.AdminPayload(), nil
}

func (a *LocalAuthenticator) AuthUserAuth(_ string) (interface{}, error) {
	return nil, errors.Errorf("User Authentication not allowed for local auth")
}

func (a *LocalAuthenticator) CreateAuthenticator() func(_, _ string, authenticate security.TokenAuthentication) runtime.Authenticator {
	return func(name string, _ string, authenticate security.TokenAuthentication) runtime.Authenticator {
		return security.HttpAuthenticator(func(r *http.Request) (bool, interface{}, error) {
			log := logutil.FromContext(r.Context(), a.log)

			p, err := authenticate(r.Header.Get(name))
			if err != nil {
				log.WithError(err).Error("failed to authenticate")
				if common.IsKnownError(err) {
					return true, nil, err
				}
				return true, nil, common.NewInfraError(http.StatusUnauthorized, err)
			}
			return true, p, nil
		})
	}
}

func validateToken(token string, pub crypto.PublicKey) (*jwt.Token, error) {
	parser := &jwt.Parser{ValidMethods: []string{jwt.SigningMethodES256.Alg()}}
	parsed, err := parser.Parse(token, func(t *jwt.Token) (interface{}, error) { return pub, nil })

	if err != nil {
		return nil, errors.Errorf("Failed to parse token: %v\n", err)
	}
	if !parsed.Valid {
		return nil, errors.Errorf("Invalid token")
	}

	return parsed, nil
}
