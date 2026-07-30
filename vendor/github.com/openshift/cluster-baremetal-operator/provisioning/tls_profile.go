package provisioning

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/crypto"
)

// ShouldHonorClusterTLSProfile returns true if the component should honor the
// cluster-wide TLS security profile from apiserver.config.openshift.io/cluster.
// Returns false for NoOpinion ("") and LegacyAdheringComponentsOnly; returns
// true for StrictAllComponents and any unknown value (fail-secure).
// This mirrors library-go/pkg/crypto.ShouldHonorClusterTLSProfile().
func ShouldHonorClusterTLSProfile(tlsAdherence configv1.TLSAdherencePolicy) bool {
	switch tlsAdherence {
	case configv1.TLSAdherencePolicyNoOpinion, configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly:
		return false
	default:
		return true
	}
}

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

// tlsGroupsToOpenSSLCurves converts TLSGroup values to a colon-separated
// string of OpenSSL curve names for Apache's SSLOpenSSLConfCmd Curves directive.
// All defined TLSGroup string values are directly usable as OpenSSL group names
// (requires OpenSSL >= 3.5).
// Returns the curve string and any unrecognized group names that were filtered out.
func tlsGroupsToOpenSSLCurves(groups []configv1.TLSGroup) (string, []string) {
	var supported []string
	var unsupported []string
	for _, g := range groups {
		switch g {
		case configv1.TLSGroupX25519,
			configv1.TLSGroupSecP256r1,
			configv1.TLSGroupSecP384r1,
			configv1.TLSGroupSecP521r1,
			configv1.TLSGroupX25519MLKEM768,
			configv1.TLSGroupSecP256r1MLKEM768,
			configv1.TLSGroupSecP384r1MLKEM1024:
			supported = append(supported, string(g))
		default:
			unsupported = append(unsupported, string(g))
		}
	}
	return strings.Join(supported, ":"), unsupported
}

// tlsGroupToGoName maps TLSGroup values to Go's crypto/tls CurveID constant names,
// suitable for BMO's --tls-curve-preferences flag.
// SecP256r1MLKEM768 and SecP384r1MLKEM1024 are excluded because Go's crypto/tls
// does not support them until Go 1.26 (BMO currently uses Go 1.25).
var tlsGroupToGoName = map[configv1.TLSGroup]string{
	configv1.TLSGroupX25519:         "X25519",
	configv1.TLSGroupSecP256r1:      "CurveP256",
	configv1.TLSGroupSecP384r1:      "CurveP384",
	configv1.TLSGroupSecP521r1:      "CurveP521",
	configv1.TLSGroupX25519MLKEM768: "X25519MLKEM768",
}

// tlsGroupsToGoNames converts TLSGroup values to Go crypto/tls CurveID constant
// name strings, suitable for BMO's --tls-curve-preferences flag.
// Groups not supported by Go's crypto/tls are filtered out.
// Returns the Go names and any unsupported group names that were filtered out.
func tlsGroupsToGoNames(groups []configv1.TLSGroup) ([]string, []string) {
	var names []string
	var unsupported []string
	for _, g := range groups {
		if name, ok := tlsGroupToGoName[g]; ok {
			names = append(names, name)
		} else {
			unsupported = append(unsupported, string(g))
		}
	}
	return names, unsupported
}

// LogUnsupportedTLSGroups logs any TLS groups from the profile that will be
// filtered out for Apache (OpenSSL) or BMO (Go) paths. This should be called
// once when the TLS profile is resolved, not on every container construction.
// TODO(hroyrh): Check if we can do this via controller-runtime-common.
func LogUnsupportedTLSGroups(groups []configv1.TLSGroup) {
	if len(groups) == 0 {
		return
	}
	_, unsupportedOpenSSL := tlsGroupsToOpenSSLCurves(groups)
	if len(unsupportedOpenSSL) > 0 {
		klog.Infof("TLS profile contains groups not supported by OpenSSL that will be excluded from ironic containers: %v", unsupportedOpenSSL)
	}
	_, unsupportedGo := tlsGroupsToGoNames(groups)
	if len(unsupportedGo) > 0 {
		klog.Infof("TLS profile contains groups not supported by Go (BMO) that will be excluded from baremetal-operator: %v", unsupportedGo)
	}
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

	if len(profile.Groups) > 0 {
		curvesStr, _ := tlsGroupsToOpenSSLCurves(profile.Groups)
		if curvesStr != "" {
			envVars = append(envVars,
				corev1.EnvVar{Name: "IRONIC_TLS_CURVES", Value: curvesStr},
				corev1.EnvVar{Name: "IRONIC_VMEDIA_CURVES", Value: curvesStr},
			)
		}
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
	if bmoVersion != "TLS13" {
		// Convert OpenSSL cipher names to IANA names for Go/BMO.
		// Filter out TLS 1.3 ciphers (TLS_ prefix) — they aren't configurable in Go.
		tls12Ciphers, _ := splitCiphers(profile.Ciphers)
		if len(tls12Ciphers) > 0 {
			ianaCiphers := crypto.OpenSSLToIANACipherSuites(tls12Ciphers)
			if len(ianaCiphers) > 0 {
				args = append(args, "--tls-cipher-suites", strings.Join(ianaCiphers, ","))
			}
		}
	}

	if len(profile.Groups) > 0 {
		goNames, _ := tlsGroupsToGoNames(profile.Groups)
		if len(goNames) > 0 {
			args = append(args, "--tls-curve-preferences", strings.Join(goNames, ","))
		}
	}

	return args
}
