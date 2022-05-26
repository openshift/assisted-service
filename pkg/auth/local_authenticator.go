package auth

import (
	"crypto"
	"net/http"
	"time"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/security"
	"github.com/go-openapi/strfmt"
	"github.com/golang-jwt/jwt/v4"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type LocalAuthenticator struct {
	cache     *cache.Cache
	db        *gorm.DB
	log       logrus.FieldLogger
	publicKey crypto.PublicKey
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
		cache:     cache.New(10*time.Minute, 30*time.Minute),
		db:        db,
		log:       log,
		publicKey: key,
	}

	return a, nil
}

var _ Authenticator = &LocalAuthenticator{}

func (a *LocalAuthenticator) AuthType() AuthType {
	return TypeLocal
}

func (a *LocalAuthenticator) EnableOrgTenancy() bool {
	return false
}

func (a *LocalAuthenticator) EnableOrgBasedFeatureGates() bool {
	return false
}
func (a *LocalAuthenticator) AuthAgentAuth(token string) (interface{}, error) {
	t, err := validateToken(token, a.publicKey)
	if err != nil {
		a.log.WithError(err).Error("failed to validate token")
		return nil, common.NewInfraError(http.StatusUnauthorized, err)
	}
	claims, ok := t.Claims.(jwt.MapClaims)
	if !ok {
		err := errors.Errorf("failed to parse JWT token claims")
		a.log.Error(err)
		return nil, common.NewInfraError(http.StatusUnauthorized, err)
	}

	infraEnvID, infraEnvOk := claims[string(gencrypto.InfraEnvKey)].(string)
	clusterID, clusterOk := claims[string(gencrypto.ClusterKey)].(string)
	if !infraEnvOk && !clusterOk {
		err := errors.Errorf("claims are incorrectly formatted")
		a.log.Error(err)
		return nil, common.NewInfraError(http.StatusUnauthorized, err)
	}

	if infraEnvOk {
		_, exists := a.cache.Get(infraEnvID)
		if !exists {
			if infraEnvExists(a.db, infraEnvID) {
				a.cache.Set(infraEnvID, "", cache.DefaultExpiration)
			} else {
				err := errors.Errorf("infraEnv %s does not exist", infraEnvID)
				return nil, common.NewInfraError(http.StatusUnauthorized, err)
			}
		}
		a.log.Debugf("Authenticating infraEnv %s JWT", infraEnvID)
	} else if clusterOk {
		_, exists := a.cache.Get(clusterID)
		if !exists {
			if clusterExists(a.db, clusterID) {
				a.cache.Set(clusterID, "", cache.DefaultExpiration)
			} else {
				err := errors.Errorf("Cluster %s does not exist", clusterID)
				return nil, common.NewInfraError(http.StatusUnauthorized, err)
			}
		}
		a.log.Debugf("Authenticating Cluster %s JWT", clusterID)
	}

	return ocm.AdminPayload(), nil
}

func (a *LocalAuthenticator) AuthUserAuth(_ string) (interface{}, error) {
	return nil, common.NewInfraError(http.StatusUnauthorized, errors.Errorf("User Authentication not allowed for local auth"))
}

func (a *LocalAuthenticator) AuthURLAuth(token string) (interface{}, error) {
	return a.AuthAgentAuth(token)
}

func (a *LocalAuthenticator) AuthImageAuth(_ string) (interface{}, error) {
	return nil, common.NewInfraError(http.StatusUnauthorized, errors.Errorf("Image Authentication not allowed for local auth"))
}

func (a *LocalAuthenticator) CreateAuthenticator() func(_, _ string, _ security.TokenAuthentication) runtime.Authenticator {
	return security.APIKeyAuth
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

func clusterExists(db *gorm.DB, clusterID string) bool {
	_, err := common.GetClusterFromDB(db, strfmt.UUID(clusterID), common.SkipEagerLoading)
	return err == nil
}

func infraEnvExists(db *gorm.DB, infraEnvID string) bool {
	_, err := common.GetInfraEnvFromDB(db, strfmt.UUID(infraEnvID))
	return err == nil
}
