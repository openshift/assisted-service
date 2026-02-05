package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"

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
		Expect(pubKey).ToNot(BeEmpty())
		Expect(privKey).ToNot(BeEmpty())
		Expect(err).ToNot(HaveOccurred())
		encodedPubKey := base64.StdEncoding.EncodeToString([]byte(pubKey))
		config = &Config{
			AuthType:       TypeAgentLocal,
			ECPublicKeyPEM: encodedPubKey,
		}

		a, err = NewAuthenticator(config, nil, logrus.New(), nil)
		Expect(err).ToNot(HaveOccurred())
		_, ok = a.(*AgentLocalAuthenticator)
		Expect(ok).To(BeTrue())

	})
})

var _ = Describe("TrustedProxyChecker", func() {
	Describe("NewTrustedProxyChecker", func() {
		It("returns empty checker for empty string", func() {
			checker := NewTrustedProxyChecker("")
			Expect(checker.HasTrustedProxies()).To(BeFalse())
		})

		It("parses single CIDR", func() {
			checker := NewTrustedProxyChecker("10.0.0.0/8")
			Expect(checker.HasTrustedProxies()).To(BeTrue())
			Expect(checker.IsTrusted("10.1.2.3")).To(BeTrue())
			Expect(checker.IsTrusted("11.0.0.1")).To(BeFalse())
		})

		It("parses multiple CIDRs", func() {
			checker := NewTrustedProxyChecker("10.0.0.0/8,192.168.0.0/16")
			Expect(checker.HasTrustedProxies()).To(BeTrue())
			Expect(checker.IsTrusted("10.1.2.3")).To(BeTrue())
			Expect(checker.IsTrusted("192.168.1.1")).To(BeTrue())
			Expect(checker.IsTrusted("172.16.0.1")).To(BeFalse())
		})

		It("parses single IP as /32", func() {
			checker := NewTrustedProxyChecker("127.0.0.1")
			Expect(checker.HasTrustedProxies()).To(BeTrue())
			Expect(checker.IsTrusted("127.0.0.1")).To(BeTrue())
			Expect(checker.IsTrusted("127.0.0.2")).To(BeFalse())
		})

		It("handles whitespace in CIDR list", func() {
			checker := NewTrustedProxyChecker("  10.0.0.0/8  , 192.168.0.0/16  ")
			Expect(checker.HasTrustedProxies()).To(BeTrue())
			Expect(checker.IsTrusted("10.1.2.3")).To(BeTrue())
			Expect(checker.IsTrusted("192.168.1.1")).To(BeTrue())
		})

		It("skips invalid entries", func() {
			checker := NewTrustedProxyChecker("invalid,10.0.0.0/8")
			Expect(checker.HasTrustedProxies()).To(BeTrue())
			Expect(checker.IsTrusted("10.1.2.3")).To(BeTrue())
		})

		It("handles IPv6 CIDR", func() {
			checker := NewTrustedProxyChecker("::1/128,fe80::/10")
			Expect(checker.HasTrustedProxies()).To(BeTrue())
			Expect(checker.IsTrusted("::1")).To(BeTrue())
			Expect(checker.IsTrusted("fe80::1")).To(BeTrue())
			Expect(checker.IsTrusted("2001:db8::1")).To(BeFalse())
		})
	})

	Describe("IsTrusted", func() {
		It("returns false for empty checker", func() {
			checker := NewTrustedProxyChecker("")
			Expect(checker.IsTrusted("10.0.0.1")).To(BeFalse())
		})

		It("returns false for invalid IP", func() {
			checker := NewTrustedProxyChecker("10.0.0.0/8")
			Expect(checker.IsTrusted("not-an-ip")).To(BeFalse())
		})
	})
})

var _ = Describe("ExtractClientIPFromRequest", func() {
	It("returns RemoteAddr when no trusted proxies configured", func() {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("X-Forwarded-For", "10.0.0.1")

		ip := ExtractClientIPFromRequest(req, nil)
		Expect(ip).To(Equal("192.168.1.100"))
	})

	It("returns RemoteAddr when request not from trusted proxy", func() {
		checker := NewTrustedProxyChecker("10.0.0.0/8")
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.1")

		ip := ExtractClientIPFromRequest(req, checker)
		Expect(ip).To(Equal("192.168.1.100"))
	})

	It("returns X-Forwarded-For when request from trusted proxy", func() {
		checker := NewTrustedProxyChecker("10.0.0.0/8")
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.1.2.3:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.1")

		ip := ExtractClientIPFromRequest(req, checker)
		Expect(ip).To(Equal("203.0.113.1"))
	})

	It("returns first IP from X-Forwarded-For chain", func() {
		checker := NewTrustedProxyChecker("10.0.0.0/8")
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.1.2.3:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1, 10.0.0.2")

		ip := ExtractClientIPFromRequest(req, checker)
		Expect(ip).To(Equal("203.0.113.1"))
	})

	It("returns X-Real-IP when X-Forwarded-For not set", func() {
		checker := NewTrustedProxyChecker("10.0.0.0/8")
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.1.2.3:12345"
		req.Header.Set("X-Real-IP", "203.0.113.1")

		ip := ExtractClientIPFromRequest(req, checker)
		Expect(ip).To(Equal("203.0.113.1"))
	})

	It("returns RemoteAddr when no forwarded headers present", func() {
		checker := NewTrustedProxyChecker("10.0.0.0/8")
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.1.2.3:12345"

		ip := ExtractClientIPFromRequest(req, checker)
		Expect(ip).To(Equal("10.1.2.3"))
	})

	It("handles IPv6 RemoteAddr", func() {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "[::1]:12345"

		ip := ExtractClientIPFromRequest(req, nil)
		Expect(ip).To(Equal("::1"))
	})
})
