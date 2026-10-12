package tlsconfig

import (
	"crypto/tls"
	"strconv"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
)

var _ = Describe("BuildTLSConfigFromCLIArgs", func() {
	It("returns TLS 1.2 config with Intermediate ciphers", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS12",
			"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(config.CipherSuites).To(HaveLen(2))
		Expect(config.CipherSuites).To(ContainElement(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256))
		Expect(config.CipherSuites).To(ContainElement(tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384))
		Expect(config.CurvePreferences).To(BeNil())
	})

	It("returns TLS 1.3 config with no cipher suites", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS13", "", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
		Expect(config.CipherSuites).To(BeNil())
	})

	It("returns TLS 1.0 config", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS10",
			"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.MinVersion).To(Equal(uint16(tls.VersionTLS10)))
		Expect(config.CipherSuites).To(HaveLen(1))
	})

	It("ignores cipher suites for TLS 1.3 and still applies curve preferences", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS13",
			"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "29,23")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
		Expect(config.CipherSuites).To(BeNil())
		Expect(config.CurvePreferences).To(Equal([]tls.CurveID{tls.X25519, tls.CurveP256}))
	})

	It("returns error for invalid TLS version", func() {
		_, err := BuildTLSConfigFromCLIArgs("InvalidVersion", "", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid TLS version"))
	})

	It("skips unsupported cipher suites without error", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS12",
			"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,FAKE_CIPHER", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.CipherSuites).To(HaveLen(1))
		Expect(config.CipherSuites).To(ContainElement(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256))
	})

	It("returns error when all cipher suites are unsupported", func() {
		_, err := BuildTLSConfigFromCLIArgs("VersionTLS12", "FAKE_CIPHER_1,FAKE_CIPHER_2", "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("none of the specified cipher suites"))
	})

	It("handles empty cipher suites string for TLS 1.2", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS12", "", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(config.CipherSuites).To(BeNil())
	})

	It("trims whitespace from cipher suite names and curve preferences", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS12",
			" TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 , TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384 ",
			" 29 , 23 ")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.CipherSuites).To(HaveLen(2))
		Expect(config.CurvePreferences).To(Equal([]tls.CurveID{tls.X25519, tls.CurveP256}))
	})

	It("skips unsupported curve preferences without error", func() {
		config, err := BuildTLSConfigFromCLIArgs("VersionTLS12", "", "29,not-a-curve")
		Expect(err).NotTo(HaveOccurred())
		Expect(config.CurvePreferences).To(Equal([]tls.CurveID{tls.X25519}))
	})

	It("returns error when all curve preferences are unsupported", func() {
		_, err := BuildTLSConfigFromCLIArgs("VersionTLS12", "", "not-a-curve,0")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("none of the specified curve preferences"))
	})
})

var _ = Describe("CLIArgsFromProfile", func() {
	It("converts the Intermediate profile into CLI values", func() {
		spec := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
		args := CLIArgsFromProfile(spec)

		Expect(args.MinVersion).To(Equal(string(configv1.VersionTLS12)))
		Expect(args.CipherSuites).To(Equal(libgocrypto.OpenSSLToIANACipherSuites(spec.Ciphers)))

		curves, unrecognized := libgocrypto.TLSGroupsToCurveIDs(spec.Groups)
		Expect(unrecognized).To(BeEmpty())
		expected := make([]string, len(curves))
		for i, curve := range curves {
			expected[i] = strconv.FormatUint(uint64(curve), 10)
		}
		Expect(args.CurvePreferences).To(Equal(expected))
		Expect(args.CurvePreferences).NotTo(BeEmpty())
	})

	It("omits unrecognized TLS groups", func() {
		args := CLIArgsFromProfile(configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS12,
			Groups:        []configv1.TLSGroup{configv1.TLSGroupX25519, "not-a-group"},
		})
		Expect(args.CurvePreferences).To(Equal([]string{strconv.Itoa(int(tls.X25519))}))
	})

	It("exposes curve preferences as an env var and a CLI flag", func() {
		args := CLIArgsFromProfile(configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS12,
			Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			Groups:        []configv1.TLSGroup{configv1.TLSGroupX25519, configv1.TLSGroupSecP256r1},
		})

		Expect(args.Env()).To(HaveKeyWithValue("TLS_MIN_VERSION", "VersionTLS12"))
		Expect(args.Env()).To(HaveKeyWithValue("TLS_CIPHER_SUITES", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"))
		Expect(args.Env()).To(HaveKeyWithValue("TLS_CURVE_PREFERENCES",
			strconv.Itoa(int(tls.X25519))+","+strconv.Itoa(int(tls.CurveP256))))
		Expect(args.Flags()).To(ConsistOf(
			"--tls-min-version=VersionTLS12",
			"--tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			"--tls-curve-preferences="+strconv.Itoa(int(tls.X25519))+","+strconv.Itoa(int(tls.CurveP256)),
		))
	})

	It("omits curve flags when the profile has no groups", func() {
		args := CLIArgsFromProfile(configv1.TLSProfileSpec{MinTLSVersion: configv1.VersionTLS13})
		Expect(args.Env()).NotTo(HaveKey("TLS_CURVE_PREFERENCES"))
		Expect(args.Flags()).To(Equal([]string{"--tls-min-version=VersionTLS13"}))
	})
})

var _ = Describe("DefaultTLSConfig", func() {
	It("uses the Intermediate profile", func() {
		result := DefaultTLSConfig()
		Expect(result.TLSProfileSpec.MinTLSVersion).To(Equal(configv1.VersionTLS12))
		Expect(result.CLIArgs().MinVersion).To(Equal(string(configv1.VersionTLS12)))
		Expect(result.CLIArgs().CurvePreferences).NotTo(BeEmpty())
		Expect(result.TLSConfig).NotTo(BeNil())
	})
})
