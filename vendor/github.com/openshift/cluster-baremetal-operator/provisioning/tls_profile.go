package provisioning

import (
	"strings"

	corev1 "k8s.io/api/core/v1"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/crypto"
)

// tlsVersionToApacheSSLProtocol maps a TLS minimum version to an Apache SSLProtocol directive value.
// The directive enables all TLS versions from the minimum up through TLS 1.3.
func tlsVersionToApacheSSLProtocol(minVersion configv1.TLSProtocolVersion) string {
	switch minVersion {
	case configv1.VersionTLS10:
		return "-ALL +TLSv1 +TLSv1.1 +TLSv1.2 +TLSv1.3"
	case configv1.VersionTLS11:
		return "-ALL +TLSv1.1 +TLSv1.2 +TLSv1.3"
	case configv1.VersionTLS13:
		return "-ALL +TLSv1.3"
	default:
		// VersionTLS12 or unrecognized — safe default
		return "-ALL +TLSv1.2 +TLSv1.3"
	}
}

// splitCiphers separates a cipher list into TLS 1.2 and TLS 1.3 ciphers.
// TLS 1.3 cipher names use the "TLS_" prefix (e.g., TLS_AES_128_GCM_SHA256).
// Everything else is a TLS 1.2 cipher in OpenSSL naming.
func splitCiphers(ciphers []string) (tls12, tls13 []string) {
	for _, c := range ciphers {
		if strings.HasPrefix(c, "TLS_") {
			tls13 = append(tls13, c)
		} else {
			tls12 = append(tls12, c)
		}
	}
	return tls12, tls13
}

// tlsProfileToApacheEnvVars returns env vars for ironic-image Apache containers.
// These set the SSLProtocol and SSLCipherSuite directives for all vhosts.
func tlsProfileToApacheEnvVars(profile configv1.TLSProfileSpec) []corev1.EnvVar {
	protocol := tlsVersionToApacheSSLProtocol(profile.MinTLSVersion)
	tls12Ciphers, tls13Ciphers := splitCiphers(profile.Ciphers)

	// Note: iPXE env vars (IPXE_SSL_PROTOCOL, IPXE_TLS_12_CIPHERS, IPXE_TLS_13_CIPHERS)
	// are intentionally omitted. iPXE firmware has a minimal TLS stack (TLS 1.2 only,
	// limited ciphers), so restricting the iPXE vhost risks breaking PXE boot.
	// The ironic-image tls-common.sh provides safe defaults for iPXE.
	envVars := []corev1.EnvVar{
		{Name: "IRONIC_SSL_PROTOCOL", Value: protocol},
		{Name: "IRONIC_VMEDIA_SSL_PROTOCOL", Value: protocol},
	}

	if len(tls12Ciphers) > 0 {
		cipherStr := strings.Join(tls12Ciphers, ":")
		envVars = append(envVars,
			corev1.EnvVar{Name: "IRONIC_TLS_12_CIPHERS", Value: cipherStr},
			corev1.EnvVar{Name: "IRONIC_VMEDIA_TLS_12_CIPHERS", Value: cipherStr},
		)
	}

	if len(tls13Ciphers) > 0 {
		cipherStr := strings.Join(tls13Ciphers, ":")
		envVars = append(envVars,
			corev1.EnvVar{Name: "IRONIC_TLS_13_CIPHERS", Value: cipherStr},
			corev1.EnvVar{Name: "IRONIC_VMEDIA_TLS_13_CIPHERS", Value: cipherStr},
		)
	}

	return envVars
}

// tlsProfileToBMOArgs returns CLI args for the BMO container.
// BMO only accepts TLS12 and TLS13 for --tls-min-version (rejects TLS10/TLS11),
// so older versions are clamped to TLS12.
// Go's crypto/tls does not allow configuring TLS 1.3 cipher suites, so
// --tls-cipher-suites is omitted when the minimum version is TLS13.
func tlsProfileToBMOArgs(profile configv1.TLSProfileSpec) []string {
	// Clamp min version: BMO rejects TLS 1.0 and 1.1
	bmoVersion := "TLS12"
	if profile.MinTLSVersion == configv1.VersionTLS13 {
		bmoVersion = "TLS13"
	}

	args := []string{"--tls-min-version", bmoVersion}

	// When min version is TLS 1.3, Go ignores cipher suite configuration entirely.
	// Skip --tls-cipher-suites to avoid BMO warnings.
	if bmoVersion == "TLS13" {
		return args
	}

	// Convert OpenSSL cipher names to IANA names for Go/BMO.
	// Filter out TLS 1.3 ciphers (TLS_ prefix) — they aren't configurable in Go.
	tls12Ciphers, _ := splitCiphers(profile.Ciphers)
	if len(tls12Ciphers) > 0 {
		ianaCiphers := crypto.OpenSSLToIANACipherSuites(tls12Ciphers)
		if len(ianaCiphers) > 0 {
			args = append(args, "--tls-cipher-suites", strings.Join(ianaCiphers, ","))
		}
	}

	return args
}
