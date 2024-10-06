package mirrorregistries

import (
	"context"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/pelletier/go-toml"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	RegistryConfKey                = "registries.conf"
	RegistryCertKey                = "ca-bundle.crt"
	imageRegistryName              = "additional-registry"
	registryCertConfigMapName      = "additional-registry-certificate"
	registryCertConfigMapNamespace = "openshift-config"
	imageConfigMapName             = "additional-registry-config"
	imageDigestMirrorSetKey        = "image-digest-mirror-set.json"
	imageTagMirrorSetKey           = "image-tag-mirror-set.json"
	registryCertConfigMapKey       = "additional-registry-certificate.json"
	imageConfigKey                 = "image-config.json"
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
		return nil, nil, nil, fmt.Errorf("failed to find registry key in toml tree")
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

// processMirrorRegistryConfig retrieves the mirror registry configuration from registries.conf and ca-bundle.crt
func processMirrorRegistryConfig(registriesConf, caBundleCrt string) (*hiveext.MirrorRegistryConfiguration, error) {
	if registriesConf == "" {
		return nil, nil
	}

	imageDigestMirrors, imageTagMirrors, insecure, err := GetImageRegistries(registriesConf)
	if err != nil {
		return nil, err
	}

	// Store the mirror configuration for later use during install config generation
	mirrorRegistryConfigurationInfo := &hiveext.MirrorRegistryConfigurationInfo{
		ImageDigestMirrors: imageDigestMirrors,
		ImageTagMirrors:    imageTagMirrors,
		Insecure:           insecure,
	}

	return &hiveext.MirrorRegistryConfiguration{
		MirrorRegistryConfigurationInfo: mirrorRegistryConfigurationInfo,
		CaBundleCrt:                     caBundleCrt,
		RegistriesConf:                  registriesConf,
	}, nil
}

// getUserTomlConfigMapData get registries.conf and ca-bundle.crt if exist in the provided configmap inside AgentClusterInstall
func getUserTomlConfigMapData(ctx context.Context, log logrus.FieldLogger, c client.Client, ref *hiveext.MirrorRegistryConfigMapReference) (string, string, error) {
	userTomlConfigMap := &corev1.ConfigMap{}
	err := c.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, userTomlConfigMap)
	if err != nil {
		log.Error(err, "Failed to get ConfigMap", "ConfigMapName", ref.Name, "ConfigMapNamespace", ref.Namespace)
		return "", "", errors.Wrap(err, "Failed to get referenced ConfigMap")
	}

	// Validating that both registries.conf and ca-bundle.crt exists
	registriesConf, ok := userTomlConfigMap.Data[RegistryConfKey]
	if !ok {
		return "", "", fmt.Errorf("ConfigMap %s/%s does not contain registries.conf key", ref.Namespace, ref.Name)
	}

	// Additional certificate is optional
	caBundleCrt := userTomlConfigMap.Data[RegistryCertKey]

	return registriesConf, caBundleCrt, nil
}

// ProcessMirrorRegistryConfig retrieves the mirror registry configuration from the referenced ConfigMap
func ProcessMirrorRegistryConfig(ctx context.Context, log logrus.FieldLogger, c client.Client, ref *hiveext.MirrorRegistryConfigMapReference) (*hiveext.MirrorRegistryConfiguration, error) {
	if ref == nil {
		return nil, nil
	}

	registriesConf, caBundleCrt, err := getUserTomlConfigMapData(ctx, log, c, ref)
	if err != nil {
		return nil, err
	}

	if registriesConf == "" && caBundleCrt == "" {
		return nil, nil
	}

	mirrorRegistryConfiguration, err := processMirrorRegistryConfig(registriesConf, caBundleCrt)
	if err != nil {
		log.Error(err, "Failed to validate and parse registries.conf")
		return nil, err
	}

	log.Info("Successfully retrieved mirror registry configuration", "ConfigMapName", ref.Name, "ConfigMapNamespace", ref.Namespace)
	return mirrorRegistryConfiguration, nil
}
