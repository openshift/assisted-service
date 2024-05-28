package mirrorregistries

import (
	"fmt"
	"os"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/pelletier/go-toml"
)

//go:generate mockgen -source=generator.go -package=mirrorregistries -destination=mock_generator.go
type MirrorRegistriesConfigBuilder interface {
	IsMirrorRegistriesConfigured() bool
	GetMirrorCA() ([]byte, error)
	GetMirrorRegistries() ([]byte, error)
	ExtractLocationMirrorDataFromRegistries() ([]RegistriesConf, error)
}

type mirrorRegistriesConfigBuilder struct {
	MirrorRegistriesConfigPath      string
	MirrorRegistriesCertificatePath string
}

func New() MirrorRegistriesConfigBuilder {
	return &mirrorRegistriesConfigBuilder{
		MirrorRegistriesConfigPath:      common.MirrorRegistriesConfigPath,
		MirrorRegistriesCertificatePath: common.MirrorRegistriesCertificatePath,
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

// return error if the path is actually an empty dir, which will indicate that
// the mirror registries are not configured.
// empty dir is due to the way we mao configmap in the assisted-service pod
func (m *mirrorRegistriesConfigBuilder) GetMirrorCA() ([]byte, error) {
	return os.ReadFile(m.MirrorRegistriesCertificatePath)
}

// returns error if the file is not present, which will also indicate that
// mirror registries are not confgiured
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
	tomlTree, err := toml.Load(registriesConfToml)
	if err != nil {
		return nil, err
	}

	registriesTree, ok := tomlTree.Get("registry").([]*toml.Tree)
	if !ok {
		return nil, fmt.Errorf("failed to cast registry key to toml Tree, registriesConfToml: %s", registriesConfToml)
	}
	registriesConfList := make([]RegistriesConf, len(registriesTree))
	for i, registryTree := range registriesTree {
		location, ok := registryTree.Get("location").(string)
		if !ok {
			return nil, fmt.Errorf("failed to cast location key to string, registriesConfToml: %s", registriesConfToml)
		}
		mirrorTree, ok := registryTree.Get("mirror").([]*toml.Tree)
		if !ok {
			return nil, fmt.Errorf("failed to cast mirror key to toml Tree, registriesConfToml: %s", registriesConfToml)
		}
		var mirrors []string
		for i := range mirrorTree {
			currentMirror, ok := mirrorTree[i].Get("location").(string)
			if !ok {
				return nil, fmt.Errorf("failed to cast mirror location key to string, registriesConfToml: %s", registriesConfToml)
			}
			mirrors = append(mirrors, currentMirror)
		}
		registriesConfList[i] = RegistriesConf{Location: location, Mirror: mirrors}
	}

	return registriesConfList, nil
}
