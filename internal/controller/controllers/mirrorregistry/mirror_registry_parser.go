package mirrorregistry

import (
	"context"
	"fmt"

	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	common2 "github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	RegistryConfKey = "registries.conf"
	RegistryCertKey = "ca-bundle.crt"
)

// getUserTomlConfigMapData get registries.conf and ca-bundle.crt if exist in the provided configmap inside AgentClusterInstall
func getUserTomlConfigMapData(ctx context.Context, log logrus.FieldLogger, c client.Client, ref *hiveext.MirrorRegistryConfigMapReference) (string, string, *corev1.ConfigMap, error) {
	userTomlConfigMap := &corev1.ConfigMap{}
	namespacedName := types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}
	err := c.Get(ctx, namespacedName, userTomlConfigMap)
	if err != nil {
		log.Error(err, "Failed to get ConfigMap", "ConfigMapName", ref.Name, "ConfigMapNamespace", ref.Namespace)
		return "", "", nil, errors.Wrap(err, "Failed to get referenced ConfigMap")
	}

	// Validating that both registries.conf and ca-bundle.crt exists
	registriesConf, ok := userTomlConfigMap.Data[RegistryConfKey]
	if !ok {
		return "", "", nil, fmt.Errorf("ConfigMap %s/%s does not contain registries.conf key", ref.Namespace, ref.Name)
	}

	// Additional certificate is optional
	caBundleCrt := userTomlConfigMap.Data[RegistryCertKey]

	log.Infof("Successfully fetched the mirror registry TOML configuration file: %s %s", ref.Namespace, ref.Name)
	return registriesConf, caBundleCrt, userTomlConfigMap, nil
}

// processMirrorRegistryConfig retrieves the mirror registry configuration from registries.conf and ca-bundle.crt
func processMirrorRegistryConfig(registriesConf, caBundleCrt string) (*common2.MirrorRegistryConfiguration, error) {
	if registriesConf == "" {
		return nil, nil
	}

	imageDigestMirrors, imageTagMirrors, insecure, err := mirrorregistries.GetImageRegistries(registriesConf)
	if err != nil {
		return nil, err
	}

	return &common2.MirrorRegistryConfiguration{
		ImageDigestMirrors: imageDigestMirrors,
		ImageTagMirrors:    imageTagMirrors,
		Insecure:           insecure,
		CaBundleCrt:        caBundleCrt,
		RegistriesConf:     registriesConf,
	}, nil
}

// ProcessMirrorRegistryConfig retrieves the mirror registry configuration from the referenced ConfigMap
func ProcessMirrorRegistryConfig(ctx context.Context, log logrus.FieldLogger, c client.Client, ref *hiveext.MirrorRegistryConfigMapReference) (*common2.MirrorRegistryConfiguration, *corev1.ConfigMap, error) {
	if ref == nil {
		return nil, nil, nil
	}

	log.Infof("Getting cluster mirror registry configurations %s %s ", ref.Namespace, ref.Name)
	registriesConf, caBundleCrt, userTomlConfigMap, err := getUserTomlConfigMapData(ctx, log, c, ref)
	if err != nil {
		return nil, nil, err
	}

	if registriesConf == "" && caBundleCrt == "" {
		log.Infof("No registires.conf ConfigMap %s/%s found", ref.Namespace, ref.Name)
		return nil, nil, nil
	}

	mirrorRegistryConfiguration, err := processMirrorRegistryConfig(registriesConf, caBundleCrt)
	if err != nil {
		log.Error("Failed to validate and parse registries.conf", err)
		return nil, nil, err
	}

	log.Info("Successfully retrieved mirror registry configuration", "ConfigMapName", ref.Name, "ConfigMapNamespace", ref.Namespace)
	return mirrorRegistryConfiguration, userTomlConfigMap, nil
}
