package gencrypto

import (
	"os"

	"github.com/dgrijalva/jwt-go"
	"github.com/pkg/errors"
)

func LocalJWT(cluster_id string) (string, error) {
	key, ok := os.LookupEnv("EC_PRIVATE_KEY_PEM")
	if !ok || key == "" {
		return "", errors.Errorf("EC_PRIVATE_KEY_PEM not found")
	}
	return LocalJWTForKey(cluster_id, key)
}

func LocalJWTForKey(cluster_id string, private_key_pem string) (string, error) {
	priv, err := jwt.ParseECPrivateKeyFromPEM([]byte(private_key_pem))
	if err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"cluster_id": cluster_id,
	})

	tokenString, err := token.SignedString(priv)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}
