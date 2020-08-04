package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base32"
	"fmt"

	"gopkg.in/square/go-jose.v2"
	"gopkg.in/square/go-jose.v2/json"
)

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
