package auth

import (
	"bytes"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"

	"github.com/dgrijalva/jwt-go"
	"github.com/sirupsen/logrus"
)

type AUtilsInteface interface {
	proccessPublicKeys(cas *x509.CertPool) (map[string]*rsa.PublicKey, error)
}

func NewAuthUtils(JwkCert string, JwkCertURL string) AUtilsInteface {
	return &aUtils{
		JwkCert:    JwkCert,
		JwkCertURL: JwkCertURL,
	}
}

type aUtils struct {
	JwkCert    string
	JwkCertURL string
}

// jwtCert on jwt key
type jwtCert struct {
	KID string `json:"kid,omitempty"`
	Kty string `json:"kty,omitempty"`
	Alg string `json:"alg,omitempty"`
	Use string `json:"use,omitempty"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
}

// jwtKeys a list of JwtCerts
type jwtKeys struct {
	Keys []jwtCert `json:"keys"`
}

func (au *aUtils) proccessPublicKeys(cas *x509.CertPool) (keyMap map[string]*rsa.PublicKey, err error) {
	var body []byte
	var certs jwtKeys
	var res *http.Response
	var pemStr string

	// Init KeyMap
	keyMap = map[string]*rsa.PublicKey{}

	if au.JwkCert != "" {
		// Use locally provided Cert
		logrus.Infof("Using locally provided Cert %s", au.JwkCert)
		err = json.Unmarshal([]byte(au.JwkCert), &certs)
		if err != nil {
			return nil, fmt.Errorf("error unmarshaling local JwkCert: %e", err)
		}
	} else {
		// Download the JSON token signing certificates:
		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: cas,
				},
			},
		}
		logrus.Infof("Getting JWK public key from %s", au.JwkCertURL)
		res, err = client.Get(au.JwkCertURL)
		if err != nil {
			return nil, fmt.Errorf("unable to download JwkCert: %e", err)
		}

		// Try to read the response body.
		body, err = ioutil.ReadAll(res.Body)
		if err != nil {
			return nil, fmt.Errorf("unable to read response body: %e", err)
		}

		// Try to parse the response body.
		err = json.Unmarshal(body, &certs)
		if err != nil {
			return nil, fmt.Errorf("error unmarshaling response body: %e", err)
		}
	}
	// Convert cert list to map.
	for _, c := range certs.Keys {
		var pubKey *rsa.PublicKey

		// Try to convert cert to string.
		pemStr, err = au.certToPEM(c)
		if err != nil {
			return nil, fmt.Errorf("error converting cert to string: %e", err)
		}

		pubKey, err = jwt.ParseRSAPublicKeyFromPEM([]byte(pemStr))
		if err != nil {
			return nil, fmt.Errorf("error parsing PEM: %e", err)
		}
		keyMap[c.KID] = pubKey
	}
	return
}

// certToPEM convert JWT object to PEM
func (au *aUtils) certToPEM(c jwtCert) (string, error) {
	var out bytes.Buffer

	// Check key type.
	if c.Kty != "RSA" {
		return "", fmt.Errorf("invalid key type: %s", c.Kty)
	}

	// Decode the base64 bytes for e and n.
	nb, err := base64.RawURLEncoding.DecodeString(c.N)
	if err != nil {
		return "", err
	}
	eb, err := base64.RawURLEncoding.DecodeString(c.E)
	if err != nil {
		return "", err
	}

	// Generate new public key
	pk := &rsa.PublicKey{
		N: new(big.Int).SetBytes(nb),
		E: int(new(big.Int).SetBytes(eb).Int64()),
	}

	der, err := x509.MarshalPKIXPublicKey(pk)
	if err != nil {
		return "", err
	}

	block := &pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: der,
	}

	// Output pem as string
	err = pem.Encode(&out, block)
	if err != nil {
		return "", err
	}

	return out.String(), nil
}
