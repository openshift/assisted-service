package gencrypto

import (
	"net/url"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/pkg/errors"
)

type LocalJWTKeyType string

const (
	InfraEnvKey LocalJWTKeyType = "infra_env_id"
	ClusterKey  LocalJWTKeyType = "cluster_id"
)

type CryptoPair struct {
	JWTKeyType  LocalJWTKeyType
	JWTKeyValue string
}

func LocalJWT(id string, keyType LocalJWTKeyType) (string, error) {
	key, ok := os.LookupEnv("EC_PRIVATE_KEY_PEM")
	if !ok || key == "" {
		return "", errors.Errorf("EC_PRIVATE_KEY_PEM not found")
	}
	return LocalJWTForKey(id, key, keyType)
}

func LocalJWTForKey(id string, private_key_pem string, keyType LocalJWTKeyType) (string, error) {
	priv, err := jwt.ParseECPrivateKeyFromPEM([]byte(private_key_pem))
	if err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		string(keyType): id,
	})

	tokenString, err := token.SignedString(priv)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func SignURL(urlString string, id string, keyType LocalJWTKeyType) (string, error) {
	tok, err := LocalJWT(id, keyType)
	if err != nil {
		return "", err
	}

	return SignURLWithToken(urlString, "api_key", tok)
}

func JWTForSymmetricKey(key []byte, expiration time.Duration, sub string) (string, error) {
	exp := time.Now().Add(expiration).Unix()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": exp,
		"sub": sub,
	})

	return token.SignedString(key)
}

func SignURLWithToken(urlString string, queryKey string, token string) (string, error) {
	u, err := url.Parse(urlString)
	if err != nil {
		return "", err
	}

	q := u.Query()
	q.Set(queryKey, token)
	u.RawQuery = q.Encode()

	return u.String(), nil
}
