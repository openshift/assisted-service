package gencrypto

import (
	"net/url"
	"os"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/golang-jwt/jwt/v4"
	"github.com/pkg/errors"
)

type LocalJWTKeyType string

const (
	InfraEnvKey LocalJWTKeyType = "infra_env_id"
	ClusterKey  LocalJWTKeyType = "cluster_id"
)

// DefaultTokenExpiration is the default expiration time for local JWT tokens.
// Tokens will be valid for 1 hour from the time of creation.
const DefaultTokenExpiration = 1 * time.Hour

type CryptoPair struct {
	JWTKeyType  LocalJWTKeyType
	JWTKeyValue string
}

// LocalJWT generates a JWT token with default expiration for local authentication.
func LocalJWT(id string, keyType LocalJWTKeyType) (string, error) {
	return LocalJWTWithExpiration(id, keyType, DefaultTokenExpiration)
}

// LocalJWTWithExpiration generates a JWT token with a specified expiration duration.
func LocalJWTWithExpiration(id string, keyType LocalJWTKeyType, expiration time.Duration) (string, error) {
	key, ok := os.LookupEnv("EC_PRIVATE_KEY_PEM")
	if !ok || key == "" {
		return "", errors.Errorf("EC_PRIVATE_KEY_PEM not found")
	}
	return LocalJWTForKeyWithExpiration(id, key, keyType, expiration)
}

// LocalJWTForKey generates a JWT token with default expiration using a provided private key.
func LocalJWTForKey(id string, private_key_pem string, keyType LocalJWTKeyType) (string, error) {
	return LocalJWTForKeyWithExpiration(id, private_key_pem, keyType, DefaultTokenExpiration)
}

// LocalJWTForKeyWithExpiration generates a JWT token with a specified expiration using a provided private key.
func LocalJWTForKeyWithExpiration(id string, private_key_pem string, keyType LocalJWTKeyType, expiration time.Duration) (string, error) {
	if expiration <= 0 {
		return "", errors.Errorf("expiration must be > 0")
	}

	priv, err := jwt.ParseECPrivateKeyFromPEM([]byte(private_key_pem))
	if err != nil {
		return "", err
	}

	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		string(keyType): id,
		"iat":           now.Unix(),
		"exp":           now.Add(expiration).Unix(),
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

// ParseExpirationFromURL parses out the the `exp` claim from the `image_token` query parameter.
// It does not verify the token before parsing so it should only be used with URLs containing trusted tokens
func ParseExpirationFromURL(urlString string) (*strfmt.DateTime, error) {
	var expiresAt strfmt.DateTime
	parsedURL, err := url.Parse(urlString)
	if err != nil {
		return nil, err
	}

	if tokenString := parsedURL.Query().Get("image_token"); tokenString != "" {
		return ParseExpirationFromToken(tokenString)
	}
	return &expiresAt, nil
}

func ParseExpirationFromToken(tokenString string) (*strfmt.DateTime, error) {
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.Errorf("malformed token claims in url")
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		return nil, errors.Errorf("token missing 'exp' claim")
	}
	expTime := time.Unix(int64(exp), 0)
	expiresAt := strfmt.DateTime(expTime)

	return &expiresAt, nil
}
