package mirrorregistries

import (
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/pelletier/go-toml"
	"github.com/pkg/errors"
)

// GetImageRegistries reads a toml tree string with the structure:
//
// [[registry]]
//
//	 location = "source-registry"
//
//	   [[registry.mirror]]
//		  location = "mirror-registry"
//		  insecure = true # indicates to use an insecure connection to this mirror registry
//		  pull-from-mirror = "tag-only" # indicates to add this to an ImageTagMirrorSet
//
// It will convert it to an ImageDigestMirrorSet CR and/or an ImageTagMirrorSet.
// It will return these as marshalled JSON strings, and it will return a string list of insecure
// mirror registries (if they exist)
func GetImageRegistries(registryTOML string) ([]configv1.ImageDigestMirrors, []configv1.ImageTagMirrors, []string, error) {
	// Parse toml and add mirror registry to image digest mirror set
	tomlTree, err := toml.Load(registryTOML)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to load value of registries.conf into toml tree; incorrectly formatted toml")
	}

	registriesTree, ok := tomlTree.Get("registry").([]*toml.Tree)
	if !ok {
		return nil, nil, nil, fmt.Errorf("failed to find registry key in toml tree, registriesConfToml: %s", registryTOML)
	}

	idmsMirrors := make([]configv1.ImageDigestMirrors, 0)
	itmsMirrors := make([]configv1.ImageTagMirrors, 0)
	insecureRegistries := make([]string, 0)

	// Add each registry and its mirrors as either an image digest mirror or an image tag mirror
	for _, registry := range registriesTree {
		source, sourceExists := registry.Get("location").(string)
		mirrorTrees, mirrorExists := registry.Get("mirror").([]*toml.Tree)
		if !sourceExists || !mirrorExists {
			continue
		}
		parseMirrorRegistries(&idmsMirrors, &itmsMirrors, mirrorTrees, &insecureRegistries, source)
	}

	if len(idmsMirrors) < 1 && len(itmsMirrors) < 1 {
		return nil, nil, nil, fmt.Errorf("failed to find any image mirrors in registry.conf")
	}

	return idmsMirrors, itmsMirrors, insecureRegistries, nil
}

// parseMirrorRegistries takes a mirror registry toml tree and parses it into
// a list of image mirrors and a string list of insecure mirror registries.
func parseMirrorRegistries(
	idmsMirrors *[]configv1.ImageDigestMirrors,
	itmsMirrors *[]configv1.ImageTagMirrors,
	mirrorTrees []*toml.Tree,
	insecureRegistries *[]string,
	source string,
) {
	itmsMirror := configv1.ImageTagMirrors{}
	idmsMirror := configv1.ImageDigestMirrors{}
	for _, mirrorTree := range mirrorTrees {
		if mirror, ok := mirrorTree.Get("location").(string); ok {
			if insecure, ok := mirrorTree.Get("insecure").(bool); ok && insecure {
				*insecureRegistries = append(*insecureRegistries, mirror)
			}
			if pullFrom, ok := mirrorTree.Get("pull-from-mirror").(string); ok {
				switch pullFrom {
				case "tag-only":
					itmsMirror.Source = source
					itmsMirror.Mirrors = append(itmsMirror.Mirrors, configv1.ImageMirror(mirror))
				case "digest-only":
					idmsMirror.Source = source
					idmsMirror.Mirrors = append(idmsMirror.Mirrors, configv1.ImageMirror(mirror))
				}
			} else {
				// Default is pulling by digest
				idmsMirror.Source = source
				idmsMirror.Mirrors = append(idmsMirror.Mirrors, configv1.ImageMirror(mirror))
			}
		}
	}
	if len(itmsMirror.Mirrors) > 0 {
		*itmsMirrors = append(*itmsMirrors, itmsMirror)
	}
	if len(idmsMirror.Mirrors) > 0 {
		*idmsMirrors = append(*idmsMirrors, idmsMirror)
	}
}

func mirrorToStrings(mirrors []configv1.ImageMirror) []string {
	strMirrors := make([]string, len(mirrors))
	for i, mirror := range mirrors {
		strMirrors[i] = string(mirror)
	}
	return strMirrors
}
