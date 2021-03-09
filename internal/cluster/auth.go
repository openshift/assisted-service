package cluster

import (
	"os"

	"github.com/dgrijalva/jwt-go"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/pkg/errors"
)

func AgentToken(c *common.Cluster, authType auth.AuthType) (token string, err error) {
	switch authType {
	case auth.TypeRHSSO:
		token, err = cloudPullSecretToken(c.PullSecret)
	case auth.TypeLocal:
		token, err = localJWT(c.ID.String())
	case auth.TypeNone:
		token = ""
	default:
		err = errors.Errorf("invalid authentication type %v", authType)
	}
	return
}

func cloudPullSecretToken(pullSecret string) (string, error) {
	creds, err := validations.ParsePullSecret(pullSecret)
	if err != nil {
		return "", err
	}
	r, ok := creds["cloud.openshift.com"]
	if !ok {
		return "", errors.Errorf("Pull secret does not contain auth for cloud.openshift.com")
	}
	return r.AuthRaw, nil
}

func localJWT(id string) (string, error) {
	key, ok := os.LookupEnv("EC_PRIVATE_KEY_PEM")
	if !ok || key == "" {
		return "", errors.Errorf("EC_PRIVATE_KEY_PEM not found")
	}

	priv, err := jwt.ParseECPrivateKeyFromPEM([]byte(key))
	if err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"cluster_id": id,
	})

	tokenString, err := token.SignedString(priv)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}
