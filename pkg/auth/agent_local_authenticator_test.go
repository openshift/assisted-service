package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/sirupsen/logrus"
)

func generateToken(privateKeyPem string, expiry *time.Time) (string, error) {
	// Create the JWT claims
	claims := jwt.MapClaims{}

	// Set the expiry time if provided
	if expiry != nil {
		claims["exp"] = expiry.Unix()
	}

	// Create the token using the ES256 signing method and the claims
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)

	priv, err := jwt.ParseECPrivateKeyFromPEM([]byte(privateKeyPem))
	if err != nil {
		return "", err
	}
	// Sign the token with the provided private key
	tokenString, err := token.SignedString(priv)
	if err != nil {
		return "", err
	}
	return tokenString, nil
}

var _ = Describe("AuthAgentAuth", func() {
	var (
		a                 *AgentLocalAuthenticator
		token, privateKey string
	)

	BeforeEach(func() {

		pubKey, privKey, err := gencrypto.ECDSAKeyPairPEM()
		privateKey = privKey
		Expect(err).ToNot(HaveOccurred())

		// Encode to Base64 (Standard encoding)
		encodedPubKey := base64.StdEncoding.EncodeToString([]byte(pubKey))

		cfg := &Config{
			ECPublicKeyPEM: encodedPubKey,
		}

		token, err = generateToken(privKey, nil)
		Expect(err).ToNot(HaveOccurred())

		a, err = NewAgentLocalAuthenticator(cfg, logrus.New())
		Expect(err).ToNot(HaveOccurred())
	})

	fakeTokenAlg := func(t string) string {
		parts := strings.Split(t, ".")

		headerJSON, err := base64.RawStdEncoding.DecodeString(parts[0])
		Expect(err).ToNot(HaveOccurred())

		header := &map[string]interface{}{}
		err = json.Unmarshal(headerJSON, header)
		Expect(err).ToNot(HaveOccurred())

		// change the algorithm in an otherwise valid token
		(*header)["alg"] = "RS256"

		headerBytes, err := json.Marshal(header)
		Expect(err).ToNot(HaveOccurred())
		newHeaderString := base64.RawStdEncoding.EncodeToString(headerBytes)

		parts[0] = newHeaderString
		return strings.Join(parts, ".")
	}

	validateErrorResponse := func(err error) {
		infraError, ok := err.(*common.InfraErrorResponse)
		Expect(ok).To(BeTrue())
		Expect(infraError.StatusCode()).To(Equal(int32(401)))
	}

	It("Validates a token correctly", func() {
		_, err := a.AuthAgentAuth(token)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Fails an invalid token", func() {
		_, err := a.AuthAgentAuth(token + "asdf")
		Expect(err).To(HaveOccurred())
		validateErrorResponse(err)
	})

	It("Works with user auth", func() {
		_, err := a.AuthUserAuth(token)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Fails with URL auth", func() {
		_, err := a.AuthURLAuth(token)
		Expect(err).To(HaveOccurred())
	})

	It("Fails with image auth", func() {
		_, err := a.AuthImageAuth(token)
		Expect(err).To(HaveOccurred())
	})

	It("Works with Watcher auth", func() {
		_, err := a.AuthWatcherAuth(token)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Validates an unexpired token correctly", func() {
		expiry := time.Now().UTC().Add(30 * time.Second)
		unexpiredToken, err := generateToken(privateKey, &expiry)
		Expect(err).ToNot(HaveOccurred())
		_, err = a.AuthAgentAuth(unexpiredToken)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Fails an expired token", func() {
		expiry := time.Now().UTC().Add(-30 * time.Second)
		expiredToken, err := generateToken(privateKey, &expiry)
		Expect(err).ToNot(HaveOccurred())
		_, err = a.AuthAgentAuth(expiredToken)
		Expect(err).To(HaveOccurred())
		validateErrorResponse(err)
	})

	It("Fails a token with invalid signing method", func() {
		newTok := fakeTokenAlg(token)
		_, err := a.AuthAgentAuth(newTok)
		Expect(err).To(HaveOccurred())
		validateErrorResponse(err)
	})

	It("Fails with an RSA token", func() {
		rsaToken, _ := GetTokenAndCert(false)
		_, err := a.AuthAgentAuth(rsaToken)
		Expect(err).To(HaveOccurred())
		validateErrorResponse(err)
	})

})
