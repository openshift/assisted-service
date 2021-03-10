package auth

import (
	"fmt"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/security"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/sirupsen/logrus"
)

type AuthType string

const (
	TypeEmpty AuthType = ""
	TypeNone  AuthType = "none"
	TypeRHSSO AuthType = "rhsso"
	TypeLocal AuthType = "local"
)

type Authenticator interface {
	CreateAuthenticator() func(name, in string, authenticate security.TokenAuthentication) runtime.Authenticator
	AuthUserAuth(token string) (interface{}, error)
	AuthAgentAuth(token string) (interface{}, error)
	AuthType() AuthType
}

type Config struct {
	AuthType       AuthType `envconfig:"AUTH_TYPE" default:""`
	JwkCert        string   `envconfig:"JWKS_CERT"`
	JwkCertURL     string   `envconfig:"JWKS_URL" default:"https://api.openshift.com/.well-known/jwks.json"`
	ECPublicKeyPEM string   `envconfig:"EC_PUBLIC_KEY_PEM"`
	// Will be split with "," as separator
	AllowedDomains string   `envconfig:"ALLOWED_DOMAINS" default:""`
	AdminUsers     []string `envconfig:"ADMIN_USERS" default:""`
}

func NewAuthenticator(cfg *Config, ocmClient *ocm.Client, log logrus.FieldLogger, db *gorm.DB) (a Authenticator, err error) {
	switch cfg.AuthType {
	case TypeRHSSO:
		a = NewRHSSOAuthenticator(cfg, ocmClient, log, db)
	case TypeNone:
		a = NewNoneAuthenticator(log)
	case TypeLocal:
		a, err = NewLocalAuthenticator(cfg, log, db)
	default:
		err = fmt.Errorf("invalid authenticator type %v", cfg.AuthType)
	}
	return
}
