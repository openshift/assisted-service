package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("AuthAgentAuth", func() {
	var (
		token string
		key   string
		a     *LocalAuthenticator
	)

	BeforeEach(func() {
		var err error
		token, key, err = ECDSATokenAndKey()
		Expect(err).ToNot(HaveOccurred())

		cfg := &Config{ECPublicKeyPEM: key}

		a, err = NewLocalAuthenticator(cfg, logrus.New())
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

	It("Validates a token correctly", func() {
		_, err := a.AuthAgentAuth(token)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Fails an invalid token", func() {
		_, err := a.AuthAgentAuth(token + "asdf")
		Expect(err).To(HaveOccurred())
	})

	It("Fails all user auth", func() {
		_, err := a.AuthUserAuth(token)
		Expect(err).To(HaveOccurred())
	})

	It("Fails a token with invalid signing method", func() {
		newTok := fakeTokenAlg(token)
		_, err := a.AuthAgentAuth(newTok)
		Expect(err).To(HaveOccurred())
	})

	It("Fails with an RSA token", func() {
		rsaToken, _ := GetTokenAndCert()
		_, err := a.AuthAgentAuth(rsaToken)
		Expect(err).To(HaveOccurred())
	})
})
