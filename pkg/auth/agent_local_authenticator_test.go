package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/sirupsen/logrus"
)

var _ = Describe("AuthAgentAuth", func() {
	var (
		a     *AgentLocalAuthenticator
		token string
	)

	BeforeEach(func() {
		infraEnvID := strfmt.UUID(uuid.New().String())

		pubKey, privKey, err := gencrypto.ECDSAKeyPairPEM()
		Expect(err).ToNot(HaveOccurred())

		// Encode to Base64 (Standard encoding)
		encodedPrivateKey := base64.StdEncoding.EncodeToString([]byte(privKey))
		encodedPubKey := base64.StdEncoding.EncodeToString([]byte(pubKey))

		cfg := &Config{
			ECPublicKeyPEM:  encodedPubKey,
			ECPrivateKeyPEM: encodedPrivateKey,
		}

		token, err = gencrypto.LocalJWTForKey(infraEnvID.String(), privKey, gencrypto.InfraEnvKey)
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

	It("Works with URL auth", func() {
		_, err := a.AuthURLAuth(token)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Fails with image auth", func() {
		_, err := a.AuthImageAuth(token)
		Expect(err).To(HaveOccurred())
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
