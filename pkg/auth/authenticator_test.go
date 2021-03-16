package auth

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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
		_, cert := GetTokenAndCert()
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
		_, ecdsaPubKey, err := ECDSATokenAndKey("")
		Expect(err).ToNot(HaveOccurred())
		config = &Config{
			AuthType:       TypeLocal,
			ECPublicKeyPEM: ecdsaPubKey,
		}

		a, err = NewAuthenticator(config, nil, logrus.New(), nil)
		Expect(err).ToNot(HaveOccurred())
		_, ok = a.(*LocalAuthenticator)
		Expect(ok).To(BeTrue())
	})
})
