package mirrorregistries

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/pelletier/go-toml"
)

//go:generate mockgen -source=generator.go -package=mirrorregistries -destination=mock_generator.go
type ServiceMirrorRegistriesConfigBuilder interface {
	IsMirrorRegistriesConfigured() bool
	GetMirrorCA() ([]byte, error)
	GetMirrorRegistries() ([]byte, error)
	ExtractLocationMirrorDataFromRegistries() ([]RegistriesConf, error)
	GenerateInsecurePolicyJSON() (string, error)
}

type mirrorRegistriesConfigBuilder struct {
	MirrorRegistriesConfigPath      string
	MirrorRegistriesCertificatePath string
	SystemCertificateBundlePath     string
	ForceInsecurePolicy             bool
}

func New(forceInsecurePolicy bool) ServiceMirrorRegistriesConfigBuilder {
	return &mirrorRegistriesConfigBuilder{
		MirrorRegistriesConfigPath:      common.MirrorRegistriesConfigPath,
		MirrorRegistriesCertificatePath: common.MirrorRegistriesCertificatePath,
		SystemCertificateBundlePath:     common.SystemCertificateBundlePath,
		ForceInsecurePolicy:             forceInsecurePolicy,
	}
}

type RegistriesConf struct {
	Location string
	Mirror   []string
}

// IsMirrorRegistriesConfigured We consider mirror registries to be configured if the following conditions are all met
//   - CA bundle file (e.g. /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem) exists
//   - registry configuration file (e.g. /etc/containers/registries.conf) exists
//   - registry configuration contains "[[registry]]" section
func (m *mirrorRegistriesConfigBuilder) IsMirrorRegistriesConfigured() bool {
	_, err := m.GetMirrorCA()
	if err != nil {
		return false
	}
	contents, err := m.GetMirrorRegistries()
	if err != nil {
		return false
	}

	tomlTree, err := toml.Load(string(contents))
	if err != nil {
		return false
	}

	_, ok := tomlTree.Get("registry").([]*toml.Tree)
	return ok
}

// GetMirrorCA return error if the path is actually an empty dir, which will indicate that
// the mirror registries are not configured.
// empty dir is due to the way we mao configmap in the assisted-service pod
func (m *mirrorRegistriesConfigBuilder) GetMirrorCA() ([]byte, error) {
	bytes, err := os.ReadFile(m.MirrorRegistriesCertificatePath)
	if err != nil {
		// fallback to tls-ca-bundle.pem (used by ABI)
		return os.ReadFile(m.SystemCertificateBundlePath)
	}
	return bytes, nil
}

// GetMirrorRegistries returns error if the file is not present, which will also indicate that
// mirror registries are not configured
func (m *mirrorRegistriesConfigBuilder) GetMirrorRegistries() ([]byte, error) {
	return os.ReadFile(m.MirrorRegistriesConfigPath)
}

func (m *mirrorRegistriesConfigBuilder) ExtractLocationMirrorDataFromRegistries() ([]RegistriesConf, error) {
	contents, err := m.GetMirrorRegistries()
	if err != nil {
		return nil, err
	}
	return ExtractLocationMirrorDataFromRegistriesFromToml(string(contents))
}

func ExtractLocationMirrorDataFromRegistriesFromToml(registriesConfToml string) ([]RegistriesConf, error) {
	var registriesConfList []RegistriesConf

	idmsMirrors, _, _, err := GetImageRegistries(registriesConfToml)
	if err != nil {
		return nil, err
	}
	for _, i := range idmsMirrors {
		registriesConfList = append(registriesConfList, RegistriesConf{Location: i.Source, Mirror: mirrorToStrings(i.Mirrors)})
	}

	return registriesConfList, nil
}

// GenerateInsecurePolicyJSON returns a base64 encoded minimal policy.json that disables signature enforcement
func (m *mirrorRegistriesConfigBuilder) GenerateInsecurePolicyJSON() (string, error) {
	if !m.ForceInsecurePolicy {
		return "", nil
	}

	policy := map[string]interface{}{
		"default": []map[string]string{
			{"type": "insecureAcceptAnything"},
		},
		"transports": map[string]interface{}{
			"docker-daemon": map[string]interface{}{
				"": []map[string]string{
					{"type": "insecureAcceptAnything"},
				},
			},
			"docker": map[string]interface{}{
				"": []map[string]string{
					{"type": "insecureAcceptAnything"},
				},
				"registry.redhat.io": []map[string]string{
					{"type": "insecureAcceptAnything"},
				},
				"registry.access.redhat.com": []map[string]string{
					{"type": "insecureAcceptAnything"},
				},
			},
		},
	}

	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal policy.json: %w", err)
	}

	return base64.StdEncoding.EncodeToString(data), nil
}
