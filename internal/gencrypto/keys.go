package gencrypto

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"strings"

	"golang.org/x/crypto/ssh"
)

func ECDSAKeyPairPEM() (string, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}

	// encode private key to PEM string
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", err
	}

	block := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privBytes,
	}

	var privKeyPEM bytes.Buffer
	err = pem.Encode(&privKeyPEM, block)
	if err != nil {
		return "", "", err
	}

	// encode public key to PEM string
	pubBytes, err := x509.MarshalPKIXPublicKey(priv.Public())
	if err != nil {
		return "", "", err
	}

	block = &pem.Block{
		Type:  "EC PUBLIC KEY",
		Bytes: pubBytes,
	}

	var pubKeyPEM bytes.Buffer
	err = pem.Encode(&pubKeyPEM, block)
	if err != nil {
		return "", "", err
	}

	return pubKeyPEM.String(), privKeyPEM.String(), nil
}

// SSHKeyPair generates an ECDSA P-256 SSH keypair, which is allowed in FIPS mode.
// It returns the public key in authorized_keys format and the private key in OpenSSH PEM format.
func SSHKeyPair() (string, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}

	pub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		return "", "", err
	}

	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", "", err
	}

	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))), string(pem.EncodeToMemory(block)), nil
}

// HMACKey generates a hex string representing n random bytes
//
// This string is intended to be used as a private key for signing and
// verifying jwt tokens. Specifically ones used for downloading images
// when using rhsso auth and the image service.
func HMACKey(n int) (string, error) {
	buf := make([]byte, n)
	_, err := rand.Read(buf)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(buf), nil
}
