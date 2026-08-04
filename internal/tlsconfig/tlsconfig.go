package tlsconfig

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	configclientset "github.com/openshift/client-go/config/clientset/versioned"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("tlsconfig")

// FetchTLSConfig reads the TLS profile from the cluster's APIServer resource
// and returns a *tls.Config configured with the appropriate MinVersion and
// CipherSuites. Delegates to FetchTLSCLIArgs for the profile resolution and
// converts the result via BuildTLSConfigFromCLIArgs.
func FetchTLSConfig(ctx context.Context, restConfig *rest.Config) (*tls.Config, error) {
	minVersion, cipherSuites, err := FetchTLSCLIArgs(ctx, restConfig)
	if err != nil {
		return nil, err
	}
	return BuildTLSConfigFromCLIArgs(minVersion, strings.Join(cipherSuites, ","))
}

func getProfileSpec(profile *configv1.TLSSecurityProfile) (*configv1.TLSProfileSpec, error) {
	switch profile.Type {
	case configv1.TLSProfileOldType,
		configv1.TLSProfileIntermediateType,
		configv1.TLSProfileModernType:
		spec, ok := configv1.TLSProfiles[profile.Type]
		if !ok {
			return nil, fmt.Errorf("unknown built-in TLS profile type: %s", profile.Type)
		}
		return spec, nil
	case configv1.TLSProfileCustomType:
		if profile.Custom == nil {
			return nil, fmt.Errorf("custom TLS profile specified but Custom field is nil")
		}
		return &profile.Custom.TLSProfileSpec, nil
	default:
		return configv1.TLSProfiles[configv1.TLSProfileIntermediateType], nil
	}
}

// FetchTLSCLIArgs reads the TLS profile from the cluster's APIServer resource
// and returns the MinTLSVersion string and IANA cipher suite names suitable for
// passing as --tls-min-version and --tls-cipher-suites CLI flags.
func FetchTLSCLIArgs(ctx context.Context, restConfig *rest.Config) (minVersion string, cipherSuites []string, err error) {
	configClient, err := configclientset.NewForConfig(restConfig)
	if err != nil {
		return "", nil, fmt.Errorf("creating config client: %w", err)
	}

	apiserver, err := configClient.ConfigV1().APIServers().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		log.Error(err, "unable to get APIServer config, using default Intermediate profile")
		spec := configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
		return string(spec.MinTLSVersion), libgocrypto.OpenSSLToIANACipherSuites(spec.Ciphers), nil
	}

	profile := apiserver.Spec.TLSSecurityProfile
	if profile == nil {
		profile = &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileIntermediateType,
		}
	}

	spec, err := getProfileSpec(profile)
	if err != nil {
		return "", nil, err
	}

	log.Info("resolved TLS profile from APIServer", "type", profile.Type)
	return string(spec.MinTLSVersion), libgocrypto.OpenSSLToIANACipherSuites(spec.Ciphers), nil
}

// BuildTLSConfigFromCLIArgs builds a *tls.Config from a TLS version string
// and comma-separated IANA cipher suite names, as passed via environment
// variables or CLI flags.
func BuildTLSConfigFromCLIArgs(minVersion string, cipherSuites string) (*tls.Config, error) {
	ver, err := libgocrypto.TLSVersion(minVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid TLS version %q: %w", minVersion, err)
	}

	config := &tls.Config{
		MinVersion: ver,
	}

	if ver < tls.VersionTLS13 && cipherSuites != "" {
		names := strings.Split(cipherSuites, ",")
		var suites []uint16
		for _, name := range names {
			suite, csErr := libgocrypto.CipherSuite(strings.TrimSpace(name))
			if csErr != nil {
				log.Info("skipping unsupported cipher suite", "cipher", name)
				continue
			}
			suites = append(suites, suite)
		}
		if len(suites) == 0 {
			return nil, fmt.Errorf("none of the specified cipher suites are supported: %v", names)
		}
		config.CipherSuites = suites
	}

	return config, nil
}
