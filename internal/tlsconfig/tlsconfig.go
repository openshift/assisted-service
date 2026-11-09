package tlsconfig

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	crtls "github.com/openshift/controller-runtime-common/pkg/tls"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	cliflag "k8s.io/component-base/cli/flag"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = ctrl.Log.WithName("tlsconfig")

// TLSConfigResult holds the resolved TLS configuration.
type TLSConfigResult struct {
	// TLSConfig configures a tls.Config with the resolved minimum version,
	// cipher suites, and curve preferences. Intended for controller-runtime TLSOpts.
	TLSConfig func(*tls.Config)

	// TLSAdherencePolicy is the cluster-wide TLS adherence policy from
	// apiserver.config.openshift.io/cluster.
	TLSAdherencePolicy configv1.TLSAdherencePolicy

	// TLSProfileSpec is the TLS profile currently configured on the APIServer,
	// including when adherence does not require honoring it. The watcher
	// compares against this value so unrelated profile edits do not restart
	// the process until adherence requires them.
	TLSProfileSpec configv1.TLSProfileSpec

	// effective is the profile applied to servers and passed to other components.
	effective configv1.TLSProfileSpec
}

// CLIArgs are TLS settings suitable for --tls-min-version, --tls-cipher-suites,
// and --tls-curve-preferences, and for the matching environment variables.
type CLIArgs struct {
	MinVersion       string
	CipherSuites     []string
	CurvePreferences []string
}

// DefaultTLSConfig returns a TLSConfigResult using the default (Intermediate)
// TLS profile. Suitable for vanilla Kubernetes clusters.
func DefaultTLSConfig() TLSConfigResult {
	spec := *configv1.TLSProfiles[libgocrypto.DefaultTLSProfileType]
	tlsConfig, unsupported := crtls.NewTLSConfigFromProfile(spec)
	logUnsupported(unsupported)
	return TLSConfigResult{
		TLSConfig:          tlsConfig,
		TLSProfileSpec:     spec,
		effective:          spec,
		TLSAdherencePolicy: configv1.TLSAdherencePolicyNoOpinion,
	}
}

// ResolveTLSConfig returns the TLS configuration for this cluster.
// On OpenShift it reads the cluster-wide TLS profile; otherwise it returns
// the default Intermediate profile.
func ResolveTLSConfig(ctx context.Context, restConfig *rest.Config, isOpenShift bool) (TLSConfigResult, error) {
	if !isOpenShift {
		return DefaultTLSConfig(), nil
	}
	return ResolveOpenShiftTLSConfig(ctx, restConfig)
}

// ResolveOpenShiftTLSConfig reads the cluster-wide TLS profile from the
// APIServer resource and returns the appropriate TLS configuration.
func ResolveOpenShiftTLSConfig(ctx context.Context, restConfig *rest.Config) (TLSConfigResult, error) {
	scheme := runtime.NewScheme()
	if err := configv1.Install(scheme); err != nil {
		return TLSConfigResult{}, fmt.Errorf("registering configv1 scheme: %w", err)
	}

	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return TLSConfigResult{}, fmt.Errorf("creating controller-runtime client: %w", err)
	}

	return resolveOpenShiftTLSConfig(ctx, k8sClient)
}

func resolveOpenShiftTLSConfig(ctx context.Context, k8sClient client.Client) (TLSConfigResult, error) {
	adherencePolicy, err := crtls.FetchAPIServerTLSAdherencePolicy(ctx, k8sClient)
	if err != nil {
		return TLSConfigResult{}, fmt.Errorf("fetching TLS adherence policy from APIServer: %w", err)
	}

	profileSpec, err := crtls.FetchAPIServerTLSProfile(ctx, k8sClient)
	if err != nil {
		return TLSConfigResult{}, fmt.Errorf("fetching TLS profile from APIServer: %w", err)
	}

	if !libgocrypto.ShouldHonorClusterTLSProfile(adherencePolicy) {
		log.V(1).Info("TLS adherence policy does not require honoring cluster profile, using defaults",
			"policy", adherencePolicy)
		result := DefaultTLSConfig()
		result.TLSAdherencePolicy = adherencePolicy
		result.TLSProfileSpec = profileSpec
		return result, nil
	}

	log.Info("honoring cluster-wide TLS profile",
		"policy", adherencePolicy,
		"minTLSVersion", profileSpec.MinTLSVersion)

	tlsConfig, unsupported := crtls.NewTLSConfigFromProfile(profileSpec)
	logUnsupported(unsupported)

	return TLSConfigResult{
		TLSConfig:          tlsConfig,
		TLSAdherencePolicy: adherencePolicy,
		TLSProfileSpec:     profileSpec,
		effective:          profileSpec,
	}, nil
}

// FetchTLSCLIArgs resolves the TLS profile applied to servers in this cluster
// and returns values that can be passed to other components as environment
// variables or CLI flags.
func FetchTLSCLIArgs(ctx context.Context, restConfig *rest.Config) (CLIArgs, error) {
	result, err := ResolveOpenShiftTLSConfig(ctx, restConfig)
	if err != nil {
		return CLIArgs{}, err
	}
	return result.CLIArgs(), nil
}

// CLIArgs returns the effective profile as CLI and environment values.
func (r TLSConfigResult) CLIArgs() CLIArgs {
	return CLIArgsFromProfile(r.effective)
}

// CLIArgsFromProfile converts a TLS profile into min-version, IANA cipher
// suite, and numeric curve-preference values.
func CLIArgsFromProfile(spec configv1.TLSProfileSpec) CLIArgs {
	curves, _ := libgocrypto.TLSGroupsToCurveIDs(spec.Groups)
	prefs := make([]string, len(curves))
	for i, curve := range curves {
		prefs[i] = strconv.FormatUint(uint64(curve), 10)
	}
	return CLIArgs{
		MinVersion:       string(spec.MinTLSVersion),
		CipherSuites:     libgocrypto.OpenSSLToIANACipherSuites(spec.Ciphers),
		CurvePreferences: prefs,
	}
}

// Env returns the environment variables used to pass TLS settings to other
// components, in a stable order. Curve preferences are omitted when the
// profile does not set groups.
func (a CLIArgs) Env() []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "TLS_MIN_VERSION", Value: a.MinVersion},
		{Name: "TLS_CIPHER_SUITES", Value: strings.Join(a.CipherSuites, ",")},
	}
	if len(a.CurvePreferences) > 0 {
		env = append(env, corev1.EnvVar{
			Name:  "TLS_CURVE_PREFERENCES",
			Value: strings.Join(a.CurvePreferences, ","),
		})
	}
	return env
}

// Flags returns kube-apiserver style TLS flags. Cipher and curve flags are
// omitted when empty.
func (a CLIArgs) Flags() []string {
	flags := []string{fmt.Sprintf("--tls-min-version=%s", a.MinVersion)}
	if len(a.CipherSuites) > 0 {
		flags = append(flags, fmt.Sprintf("--tls-cipher-suites=%s", strings.Join(a.CipherSuites, ",")))
	}
	if len(a.CurvePreferences) > 0 {
		flags = append(flags, fmt.Sprintf("--tls-curve-preferences=%s", strings.Join(a.CurvePreferences, ",")))
	}
	return flags
}

// BuildTLSConfigFromCLIArgs builds a *tls.Config from a TLS version string,
// comma-separated IANA cipher suite names, and comma-separated numeric
// curve-preference IDs, as passed via environment variables or CLI flags.
func BuildTLSConfigFromCLIArgs(minVersion, cipherSuites, curvePreferences string) (*tls.Config, error) {
	ver, err := libgocrypto.TLSVersion(minVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid TLS version %q: %w", minVersion, err)
	}

	config := &tls.Config{
		MinVersion: ver,
	}

	curves, err := parseCurvePreferences(curvePreferences)
	if err != nil {
		return nil, err
	}
	if len(curves) > 0 {
		config.CurvePreferences = curves
	}

	if ver < tls.VersionTLS13 && cipherSuites != "" {
		names := strings.Split(cipherSuites, ",")
		var suites []uint16
		var invalid []string
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			suite, csErr := libgocrypto.CipherSuite(name)
			if csErr != nil {
				log.Info("skipping unsupported cipher suite", "cipher", name)
				invalid = append(invalid, name)
				continue
			}
			suites = append(suites, suite)
		}
		if len(suites) == 0 {
			return nil, fmt.Errorf("none of the specified cipher suites are supported: %v", invalid)
		}
		config.CipherSuites = suites
	}

	return config, nil
}

func parseCurvePreferences(raw string) ([]tls.CurveID, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	var curves []tls.CurveID
	var invalid []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 32)
		if err != nil {
			log.Info("skipping unsupported curve preference", "curve", part)
			invalid = append(invalid, part)
			continue
		}
		parsed, err := cliflag.TLSCurvePreferences([]int32{int32(id)})
		if err != nil {
			log.Info("skipping unsupported curve preference", "curve", part, "error", err)
			invalid = append(invalid, part)
			continue
		}
		curves = append(curves, parsed...)
	}
	if len(curves) == 0 && len(invalid) > 0 {
		return nil, fmt.Errorf("none of the specified curve preferences are supported: %v", invalid)
	}
	return curves, nil
}

func logUnsupported(unsupported []string) {
	if len(unsupported) > 0 {
		log.Info("some ciphers and/or groups from the TLS profile are not supported", "unsupported", unsupported)
	}
}
