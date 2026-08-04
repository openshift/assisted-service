package tlsconfig

import (
	"crypto/tls"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
)

var _ = Describe("BuildTLSConfigFromCLIArgs", func() {
	It("returns TLS 1.2 config with Intermediate ciphers", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS12",
			"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(config.CipherSuites).To(HaveLen(2))
		Expect(config.CipherSuites).To(ContainElement(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256))
		Expect(config.CipherSuites).To(ContainElement(tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384))
	})

	It("returns TLS 1.3 config with no cipher suites", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS13", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
		Expect(config.CipherSuites).To(BeNil())
	})

	It("returns TLS 1.0 config", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS10",
			"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.MinVersion).To(Equal(uint16(tls.VersionTLS10)))
		Expect(config.CipherSuites).To(HaveLen(1))
	})

	It("ignores cipher suites for TLS 1.3", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS13",
			"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
		Expect(config.CipherSuites).To(BeNil())
	})

	It("returns error for invalid TLS version", func() {
		_, err := BuildTLSConfigFromCLIArgs("InvalidVersion", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid TLS version"))
	})

	It("skips unsupported cipher suites without error", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS12",
			"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,FAKE_CIPHER")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.CipherSuites).To(HaveLen(1))
		Expect(config.CipherSuites).To(ContainElement(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256))
	})

	It("returns error when all cipher suites are unsupported", func() {
		_, err := BuildTLSConfigFromCLIArgs("VersionTLS12", "FAKE_CIPHER_1,FAKE_CIPHER_2")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("none of the specified cipher suites"))
	})

	It("handles empty cipher suites string for TLS 1.2", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS12", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(config.CipherSuites).To(BeNil())
	})

	It("trims whitespace from cipher suite names", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS12",
			" TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 , TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384 ")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.CipherSuites).To(HaveLen(2))
	})
})

var _ = Describe("getProfileSpec", func() {
	It("returns the Old spec for the Old profile", func() {
		spec, err := getProfileSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType})
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.MinTLSVersion).To(Equal(configv1.VersionTLS10))
		Expect(spec.Ciphers).NotTo(BeEmpty())
	})

	It("returns the Intermediate spec for the Intermediate profile", func() {
		spec, err := getProfileSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType})
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.MinTLSVersion).To(Equal(configv1.VersionTLS12))
		Expect(spec.Ciphers).NotTo(BeEmpty())
	})

	It("returns the Modern spec for the Modern profile", func() {
		spec, err := getProfileSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType})
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.MinTLSVersion).To(Equal(configv1.VersionTLS13))
	})

	It("returns the caller's spec for a Custom profile", func() {
		profile := &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{
				TLSProfileSpec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS12,
					Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
				},
			},
		}
		spec, err := getProfileSpec(profile)
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.MinTLSVersion).To(Equal(configv1.VersionTLS12))
		Expect(spec.Ciphers).To(ConsistOf("ECDHE-RSA-AES128-GCM-SHA256"))
	})

	It("returns an error for a Custom profile with no custom field", func() {
		_, err := getProfileSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Custom field is nil"))
	})

	It("falls back to Intermediate for an unknown profile type", func() {
		spec, err := getProfileSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileType("Unknown")})
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.MinTLSVersion).To(Equal(configv1.VersionTLS12))
	})
})
