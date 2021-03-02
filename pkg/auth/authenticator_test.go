package auth

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("ResolvedAuth", func() {
	It("works for all combinations", func() {
		cases := []struct {
			authType AuthType
			enabled  bool
			res      AuthType
		}{
			{authType: TypeEmpty, enabled: false, res: TypeNone},
			{authType: TypeEmpty, enabled: true, res: TypeRHSSO},
			{authType: TypeNone, enabled: false, res: TypeNone},
			{authType: TypeNone, enabled: true, res: TypeNone},
			{authType: TypeRHSSO, enabled: false, res: TypeRHSSO},
			{authType: TypeRHSSO, enabled: true, res: TypeRHSSO},
		}

		for _, c := range cases {
			config := Config{
				EnableAuth: c.enabled,
				AuthType:   c.authType,
			}
			Expect(config.ResolvedAuthType()).To(Equal(c.res), "expected type %s, and enabled %t to resolve to %s", c.authType, c.enabled, c.res)
		}
	})
})

var _ = Describe("NewAuthenticator", func() {
	It("returns an error if passed an invalid type", func() {
		config := &Config{AuthType: "blah"}
		_, err := NewAuthenticator(config, nil, logrus.New(), nil)
		Expect(err).To(HaveOccurred())
	})

	It("returns the correct type based on the config", func() {
		config := &Config{AuthType: TypeNone}
		a, err := NewAuthenticator(config, nil, logrus.New(), nil)
		Expect(err).ToNot(HaveOccurred())
		_, ok := a.(*NoneAuthenticator)
		Expect(ok).To(BeTrue())

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
	})
})
