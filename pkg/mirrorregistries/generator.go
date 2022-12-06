package mirrorregistries

import (
	"fmt"
	"io/ioutil"

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
}

func New() MirrorRegistriesConfigBuilder {
	return &mirrorRegistriesConfigBuilder{}
}

type RegistriesConf struct {
	Location string
	Mirror   string
}

func (m *mirrorRegistriesConfigBuilder) IsMirrorRegistriesConfigured() bool {
	_, err := m.GetMirrorCA()
	if err != nil {
		return false
	}
	content, err := m.GetMirrorRegistries()
	if err != nil {
		return false
	}
	_, err = loadRegistriesConf(string(content))
	return err == nil
}

// return error if the path is actually an empty dir, which will indicate that
// the mirror registries are not configured.
// empty dir is due to the way we mao configmap in the assisted-service pod
func (m *mirrorRegistriesConfigBuilder) GetMirrorCA() ([]byte, error) {
	return readFile(common.MirrorRegistriesCertificatePath)
}

// returns error if the file is not present, which will also indicate that
// mirror registries are not confgiured
func (m *mirrorRegistriesConfigBuilder) GetMirrorRegistries() ([]byte, error) {
	return readFile(common.MirrorRegistriesConfigPath)
}

func (m *mirrorRegistriesConfigBuilder) ExtractLocationMirrorDataFromRegistries() ([]RegistriesConf, error) {
	contents, err := m.GetMirrorRegistries()
	if err != nil {
		return nil, err
	}
	return extractLocationMirrorDataFromRegistries(string(contents))
}

func extractLocationMirrorDataFromRegistries(registriesConfToml string) ([]RegistriesConf, error) {
	registriesTree, err := loadRegistriesConf(registriesConfToml)
	if err != nil {
		return nil, err
	}
	registriesConfList := make([]RegistriesConf, len(registriesTree))
	for i, registryTree := range registriesTree {
		location, ok := registryTree.Get("location").(string)
		if !ok {
			return nil, fmt.Errorf("Failed to cast location key to string")
		}
		mirrorTree, ok := registryTree.Get("mirror").([]*toml.Tree)
		if !ok {
			return nil, fmt.Errorf("Failed to cast mirror key to toml Tree")
		}
		mirror, ok := mirrorTree[0].Get("location").(string)
		if !ok {
			return nil, fmt.Errorf("Failed to cast mirror location key to string")
		}
		registriesConfList[i] = RegistriesConf{Location: location, Mirror: mirror}
	}

	return registriesConfList, nil
}

func loadRegistriesConf(registriesConfToml string) ([]*toml.Tree, error) {
	tomlTree, err := toml.Load(registriesConfToml)
	if err != nil {
		return nil, err
	}

	registriesTree, ok := tomlTree.Get("registry").([]*toml.Tree)
	if !ok {
		return nil, fmt.Errorf("failed to cast registry key to toml Tree. content: %s", registriesConfToml)
	}
	return registriesTree, nil
}

func readFile(filePath string) ([]byte, error) {
	return ioutil.ReadFile(filePath)
}
