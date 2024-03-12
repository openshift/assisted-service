package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base32"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"gopkg.in/square/go-jose.v2"
	"gopkg.in/square/go-jose.v2/json"
)

func GetTokenAndCert(withLateIat bool) (string, []byte) {

	//Generate RSA Keypair
	pub, priv, _ := GenKeys(2048)

	//Generate keys in JWK format
	pubJSJWKS, _, kid, _ := GenJSJWKS(priv, pub)

	mapClaims := jwt.MapClaims{
		"account_number": "1234567",
		"is_internal":    false,
		"is_active":      true,
		"account_id":     "7654321",
		"org_id":         "1010101",
		"last_name":      "Doe",
		"type":           "User",
		"locale":         "en_US",
		"first_name":     "John",
		"email":          "jdoe123@example.com",
		"username":       "jdoe123@example.com",
		"is_org_admin":   false,
		"client_id":      "1234",
	}
	if withLateIat {
		mapClaims["iat"] = time.Now().Add(5 * time.Minute).Unix()
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, mapClaims)
	token.Header["kid"] = kid
	tokenString, _ := token.SignedString(priv)
	return tokenString, pubJSJWKS
}

func GenKeys(bits int) (crypto.PublicKey, crypto.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		fmt.Printf("RSA Keys Generation error: %v\n", err)
	}
	return key.Public(), key, err
}

func GenJSJWKS(privKey crypto.PublicKey, pubKey crypto.PublicKey) ([]byte, []byte, string, error) {
	var pubJSJWKS []byte
	var privJSJWKS []byte
	var err error

	alg := "RS256"
	use := "sig"

	//Generate random kid
	b := make([]byte, 10)
	_, err = rand.Read(b)
	if err != nil {
		fmt.Printf("Kid Generation error: %v\n", err)
	}
	kid := base32.StdEncoding.EncodeToString(b)

	//  Public and private keys in JWK format
	priv := jose.JSONWebKey{Key: privKey, KeyID: kid, Algorithm: alg, Use: use}
	pub := jose.JSONWebKey{Key: pubKey, KeyID: kid, Algorithm: alg, Use: use}
	privJWKS := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{priv}}
	pubJWKS := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{pub}}

	privJSJWKS, err = json.Marshal(privJWKS)
	if err != nil {
		fmt.Printf("privJSJWKS Marshaling error: %v\n", err)
	}
	pubJSJWKS, err = json.Marshal(pubJWKS)

	if err != nil {
		fmt.Printf("pubJSJWKS Marshaling error: %v\n", err)
	}
	return pubJSJWKS, privJSJWKS, kid, nil
}

func GetConfigRHSSO() *Config {
	_, cert := GetTokenAndCert(false)
	cfg := &Config{JwkCert: string(cert), AuthType: TypeRHSSO, EnableOrgTenancy: true, EnableOrgBasedFeatureGates: false}
	return cfg
}
