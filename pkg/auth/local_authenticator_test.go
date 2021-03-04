package auth

import (
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
})
