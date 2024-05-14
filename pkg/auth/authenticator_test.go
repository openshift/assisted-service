package auth

import (
	"encoding/base64"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/sirupsen/logrus"
)

var _ = Describe("NewAuthenticator", func() {
	It("returns an error if passed an invalid type", func() {
		config := &Config{AuthType: "blah"}
		_, err := NewAuthenticator(config, nil, logrus.New(), nil)
		Expect(err).To(HaveOccurred())
	})

	It("returns the correct type based on the config", func() {
		// NoneAuthenticator
		config := &Config{AuthType: TypeNone}

		a, err := NewAuthenticator(config, nil, logrus.New(), nil)
		Expect(err).ToNot(HaveOccurred())
		_, ok := a.(*NoneAuthenticator)
		Expect(ok).To(BeTrue())

		// RHSSOAuthenticator
		_, cert := GetTokenAndCert(false)
		config = &Config{
			AuthType:   TypeRHSSO,
			JwkCertURL: "",
			JwkCert:    string(cert),
		}

		a, err = NewAuthenticator(config, nil, logrus.New(), nil)
		Expect(err).ToNot(HaveOccurred())
		_, ok = a.(*RHSSOAuthenticator)
		Expect(ok).To(BeTrue())

		// LocalAuthenticator
		pubKey, _, err := gencrypto.ECDSAKeyPairPEM()
		Expect(err).ToNot(HaveOccurred())
		config = &Config{
			AuthType:       TypeLocal,
			ECPublicKeyPEM: pubKey,
		}

		a, err = NewAuthenticator(config, nil, logrus.New(), nil)
		Expect(err).ToNot(HaveOccurred())
		_, ok = a.(*LocalAuthenticator)
		Expect(ok).To(BeTrue())

		// AgentLocalAuthenticator
		pubKey, privKey, err := gencrypto.ECDSAKeyPairPEM()
		Expect(err).ToNot(HaveOccurred())
		encodedPrivateKey := base64.StdEncoding.EncodeToString([]byte(privKey))
		encodedPubKey := base64.StdEncoding.EncodeToString([]byte(pubKey))
		config = &Config{
			AuthType:        TypeAgentLocal,
			ECPublicKeyPEM:  encodedPubKey,
			ECPrivateKeyPEM: encodedPrivateKey,
		}

		a, err = NewAuthenticator(config, nil, logrus.New(), nil)
		Expect(err).ToNot(HaveOccurred())
		_, ok = a.(*AgentLocalAuthenticator)
		Expect(ok).To(BeTrue())

	})
})
