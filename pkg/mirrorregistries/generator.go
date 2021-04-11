package mirrorregistries

import (
	"fmt"

	"github.com/openshift/assisted-service/models"
	"github.com/pelletier/go-toml"
)

type RegistriesConf struct {
	Location string
	Mirror   string
}

// builds an /etc/containers/registries.conf file contents in TOML format based on the input.
// for format details see man containers-registries.conf
func FormatRegistriesConfForIgnition(mirrorRegistriesCaConfig *models.MirrorRegistriesCaConfig) (string, string, error) {
	if mirrorRegistriesCaConfig == nil {
		return "", "", nil
	}
	caConfig := mirrorRegistriesCaConfig.CaConfig
	mirrorRegistriesConfig := mirrorRegistriesCaConfig.MirrorRegistriesConfig

	// create a map that represents TOML tree
	treeMap := make(map[string]interface{})
	registryEntryList := []map[string]interface{}{}
	// loop over mirror registries data and for each create its own map
	for _, mirrorRegistryConfig := range mirrorRegistriesConfig.MirrorRegistries {
		registryMap := make(map[string]interface{})
		registryMap["prefix"] = mirrorRegistryConfig.Prefix
		registryMap["location"] = mirrorRegistryConfig.Location
		registryMap["mirror-by-digest-only"] = false
		mirrorMap := make(map[string]interface{})
		mirrorMap["location"] = mirrorRegistryConfig.MirrorLocation
		// mirror is also an  TOML array, so it must be a list of maps
		registryMap["mirror"] = []interface{}{mirrorMap}
		registryEntryList = append(registryEntryList, registryMap)
	}

	treeMap["unqualified-search-registries"] = mirrorRegistriesConfig.UnqualifiedSearchRegistries
	treeMap["registry"] = registryEntryList

	tomlTree, err := toml.TreeFromMap(treeMap)
	if err != nil {
		return "", "", err
	}
	tomlString, err := tomlTree.ToTomlString()
	if err != nil {
		return "", "", err
	}
	return tomlString, caConfig, nil
}

func ExtractLocationMirrorDataFromRegistries(registriesConfToml string) ([]RegistriesConf, error) {
	tomlTree, err := toml.Load(registriesConfToml)
	if err != nil {
		return nil, err
	}

	registriesTree, ok := tomlTree.Get("registry").([]*toml.Tree)
	if !ok {
		return nil, fmt.Errorf("Failed to cast registry key to toml Tree")
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
