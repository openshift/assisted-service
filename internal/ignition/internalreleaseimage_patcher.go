package ignition

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"strings"

	config_latest_types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/go-openapi/swag"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pelletier/go-toml"
	"github.com/sirupsen/logrus"
	"github.com/vincent-petithory/dataurl"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"
)

const (
	iriKind            = "InternalReleaseImage"
	iriInstanceName    = "cluster"
	iriRegistryPort    = 22625
	iriPatchAnnotation = "internalreleaseimage.openshift.io/patched"
	registriesConfKey  = "/etc/containers/registries.conf"
)

// internalReleaseImagePatcher takes care of patching both the oc mirror manifests
// and bootstrap.ign when the InternalReleaseImage resource was found.
// The manifests are added by the Appliance as extra manifests, but they lack the
// mirroring information for localhost/api-int. This is required as par of the support
// of the InternalReleaseImage registries managed by the related MCO controller
// (see https://github.com/openshift/machine-config-operator/blob/201cc3161a5972e88db97be816a123fb101cca6b/pkg/controller/internalreleaseimage/internalreleaseimage_controller.go#L50).
type internalReleaseImagePatcher struct {
	log               logrus.FieldLogger
	cluster           *common.Cluster
	s3Client          s3wrapper.API
	iriRegistryDomain string
	iriFound          bool
}

// NewInternalReleaseImagePatcher creates a new internalReleaseImagePatcher instance.
func NewInternalReleaseImagePatcher(cluster *common.Cluster, s3Client s3wrapper.API, log logrus.FieldLogger) internalReleaseImagePatcher {
	return internalReleaseImagePatcher{
		cluster:           cluster,
		s3Client:          s3Client,
		log:               log,
		iriRegistryDomain: fmt.Sprintf("api-int.%s.%s", cluster.Name, cluster.BaseDNSDomain),
	}
}

func (i *internalReleaseImagePatcher) patchMirror(origMirror string, host string) string {
	re := regexp.MustCompile(`^[^/]+`)
	return re.ReplaceAllString(origMirror, fmt.Sprintf("%s:%d", host, iriRegistryPort))
}

func (i *internalReleaseImagePatcher) patchImageMirror(mirror configv1.ImageMirror, host string) configv1.ImageMirror {
	return configv1.ImageMirror(i.patchMirror(string(mirror), host))
}

// mirrorHasLocalhost returns true if the mirror reference uses localhost as the host.
func mirrorHasLocalhost(m configv1.ImageMirror) bool {
	s := string(m)
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	return s == "localhost" || strings.HasPrefix(s, "localhost:")
}

// mirrorsContainLocalhost returns true if any mirror in the slice uses localhost as the host.
func mirrorsContainLocalhost(mirrors []configv1.ImageMirror) bool {
	for _, m := range mirrors {
		if mirrorHasLocalhost(m) {
			return true
		}
	}
	return false
}

func (i *internalReleaseImagePatcher) uploadToS3(ctx context.Context, key string, data []byte) error {
	i.log.Infof("Uploading resource %s as system manifest", key)
	metadata := map[string]string{
		constants.ManifestSourceAttribute: constants.ManifestSourceSystemGenerated,
	}
	return i.s3Client.UploadWithMetadata(ctx, data, key, metadata)
}

func (i *internalReleaseImagePatcher) uploadManifests(ctx context.Context, key string, obj interface{}) error {
	data, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}
	return i.uploadToS3(ctx, key, data)
}

func (i *internalReleaseImagePatcher) getManifestContent(ctx context.Context, manifest string) ([]byte, error) {
	respBody, _, err := i.s3Client.Download(ctx, manifest)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()
	content, err := io.ReadAll(respBody)
	if err != nil {
		return nil, err
	}
	return content, nil
}

func (i *internalReleaseImagePatcher) alreadyPatched(obj metav1.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}
	_, found := annotations[iriPatchAnnotation]
	return found
}

func (i *internalReleaseImagePatcher) markAsPatched(obj metav1.Object) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[iriPatchAnnotation] = ""
	obj.SetAnnotations(annotations)
}

func (i *internalReleaseImagePatcher) getInternalReleaseImageManifest(ctx context.Context, manifestFiles []s3wrapper.ObjectInfo) error {
	for _, f := range manifestFiles {
		content, err := i.getManifestContent(ctx, f.Path)
		if err != nil {
			return err
		}
		var meta struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}
		if err := yaml.Unmarshal(content, &meta); err != nil {
			continue
		}
		if meta.Kind == iriKind && meta.Metadata.Name == iriInstanceName {
			i.iriFound = true
			break
		}
	}

	return nil
}

// PatchManifests looks if the InternalReleaseImage manifest has been defined. In such case, it extends the
// IDMS/ITMS manifests (generated by oc mirror) with additional mirror entries for localhost/api-int.
// ClusterCatalog/CatalogSources resources are instead patched to consume api-int.
// All IRI-related manifests are re-uploaded as system manifests so they are not shown in the UI.
func (i *internalReleaseImagePatcher) PatchManifests(ctx context.Context, manifestFiles []s3wrapper.ObjectInfo) error {
	i.log.Infof("Looking for InternalReleaseImage mirror resources")

	// Check if the InternalReleaseImage manifest exists.
	err := i.getInternalReleaseImageManifest(ctx, manifestFiles)
	if err != nil {
		return err
	}
	// Skip if InternalReleaseImage manifest wasn't found.
	if !i.iriFound {
		return nil
	}
	i.log.Infof("Patching InternalReleaseImage mirror resources")

	// Process the oc-mirror manifests and mark all IRI-related manifests as system.
	for _, f := range manifestFiles {
		content, err := i.getManifestContent(ctx, f.Path)
		if err != nil {
			return err
		}

		u := unstructured.Unstructured{}
		_, _, err = scheme.Codecs.UniversalDecoder().Decode(content, nil, &u)
		if err != nil {
			i.log.Debugf("Skipping %s, cannot decode manifest", f.Path)
			continue
		}
		if i.alreadyPatched(&u) {
			i.log.Debugf("Skipping %s, already patched", f.Path)
			continue
		}

		switch u.GetKind() {
		case iriKind:
			if err := i.uploadManifests(ctx, f.Path, u.Object); err != nil {
				return err
			}

		case "ImageDigestMirrorSet":
			i.log.Infof("Patching ImageDigestMirrorSet manifest %s", f.Path)
			var idms configv1.ImageDigestMirrorSet
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &idms); err != nil {
				return err
			}
			i.markAsPatched(&idms)
			for j, group := range idms.Spec.ImageDigestMirrors {
				iriMirrors := []configv1.ImageMirror{}
				hasLocalhost := mirrorsContainLocalhost(group.Mirrors)
				for _, m := range group.Mirrors {
					iriMirrors = append(iriMirrors, i.patchImageMirror(m, i.iriRegistryDomain))
					if !hasLocalhost {
						iriMirrors = append(iriMirrors, i.patchImageMirror(m, "localhost"))
					}
				}
				idms.Spec.ImageDigestMirrors[j].Mirrors = append(idms.Spec.ImageDigestMirrors[j].Mirrors, iriMirrors...)
			}
			if err := i.uploadManifests(ctx, f.Path, &idms); err != nil {
				return err
			}

		case "ImageTagMirrorSet":
			i.log.Infof("Patching ImageTagMirrorSet manifest %s", f.Path)
			var itms configv1.ImageTagMirrorSet
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &itms); err != nil {
				return err
			}
			i.markAsPatched(&itms)
			for j, group := range itms.Spec.ImageTagMirrors {
				iriMirrors := []configv1.ImageMirror{}
				hasLocalhost := mirrorsContainLocalhost(group.Mirrors)
				for _, m := range group.Mirrors {
					iriMirrors = append(iriMirrors, i.patchImageMirror(m, i.iriRegistryDomain))
					if !hasLocalhost {
						iriMirrors = append(iriMirrors, i.patchImageMirror(m, "localhost"))
					}
				}
				itms.Spec.ImageTagMirrors[j].Mirrors = append(itms.Spec.ImageTagMirrors[j].Mirrors, iriMirrors...)
			}
			if err := i.uploadManifests(ctx, f.Path, &itms); err != nil {
				return err
			}

		case "ClusterCatalog":
			i.log.Infof("Patching ClusterCatalog manifest %s", f.Path)
			cc := u.DeepCopy()
			i.markAsPatched(cc)

			ref, found, err := unstructured.NestedString(cc.Object, "spec", "source", "image", "ref")
			if err != nil {
				return fmt.Errorf("error while reading ClusterCatalog resource %s: %v", cc.GetName(), err)
			}
			if !found {
				return fmt.Errorf("cannot find ref field on ClusterCatalog resource %s", cc.GetName())
			}
			newRef := i.patchMirror(ref, i.iriRegistryDomain)
			if err := unstructured.SetNestedField(cc.Object, newRef, "spec", "source", "image", "ref"); err != nil {
				return fmt.Errorf("error while decoding ClusterCatalog resource %s: %v", cc.GetName(), err)
			}
			if err := i.uploadManifests(ctx, f.Path, cc.Object); err != nil {
				return err
			}

		case "CatalogSource":
			i.log.Infof("Patching CatalogSource manifest %s", f.Path)
			cs := u.DeepCopy()
			i.markAsPatched(cs)

			image, found, err := unstructured.NestedString(cs.Object, "spec", "image")
			if err != nil {
				return fmt.Errorf("error while reading CatalogSource resource %s: %v", cs.GetName(), err)
			}
			if !found {
				return fmt.Errorf("cannot find image field on CatalogSource resource %s", cs.GetName())
			}
			newImage := i.patchMirror(image, i.iriRegistryDomain)
			if err := unstructured.SetNestedField(cs.Object, newImage, "spec", "image"); err != nil {
				return fmt.Errorf("error while decoding CatalogSource resource %s: %v", cs.GetName(), err)
			}
			if err := i.uploadManifests(ctx, f.Path, cs.Object); err != nil {
				return err
			}
		}
	}

	return nil
}

func (i *internalReleaseImagePatcher) getRegistriesConfFromIgn(bootstrapConfig *config_latest_types.Config) (string, int, error) {
	var registriesConfFile *config_latest_types.File
	var registriesConfFileIndex int

	for n, f := range bootstrapConfig.Storage.Files {
		if f.Path == registriesConfKey {
			registriesConfFile = &f
			registriesConfFileIndex = n
			break
		}
	}
	if registriesConfFile == nil {
		return "", -1, fmt.Errorf("cannot find %s in bootstrap.ign", registriesConfKey)
	}
	source := registriesConfFile.FileEmbedded1.Contents.Key()
	dataURL, err := dataurl.DecodeString(source)
	if err != nil {
		return "", -1, err
	}

	return string(dataURL.Data), registriesConfFileIndex, nil
}

func (i *internalReleaseImagePatcher) UpdateBootstrap(bootstrapConfig *config_latest_types.Config) error {
	// Skip if InternalReleaseImage manifest wasn't found.
	if !i.iriFound {
		return nil
	}
	i.log.Infof("Updating bootstrap.ign registries.conf for InternalReleaseImage")

	// Extract the registriesConf file.
	data, idx, err := i.getRegistriesConfFromIgn(bootstrapConfig)
	if err != nil {
		return err
	}

	// Parse and update the registries.conf content.
	newData, err := i.updateRegistriesConf(data)
	if err != nil {
		return err
	}

	// Update the ignition configuration.
	encodedData := swag.String("data:;base64," + base64.StdEncoding.EncodeToString([]byte(newData)))
	bootstrapConfig.Storage.Files[idx].FileEmbedded1.Contents.Source = encodedData

	return nil
}

func (i *internalReleaseImagePatcher) updateRegistriesConf(data string) (string, error) {
	registryTOML, err := toml.Load(data)
	if err != nil {
		return "", err
	}
	registriesTree, ok := registryTOML.Get("registry").([]*toml.Tree)
	if !ok {
		return "", fmt.Errorf("failed to find registry key in toml tree, registriesConfToml: %s", registryTOML)
	}
	for _, registry := range registriesTree {
		mirrorTrees, mirrorExists := registry.Get("mirror").([]*toml.Tree)
		if !mirrorExists {
			continue
		}

		// For each mirror entry of current registry, let's add new localhost/api-int entries.
		iriMirrors := []*toml.Tree{}
		for _, m := range mirrorTrees {
			location, ok := m.Get("location").(string)
			if !ok {
				return "", fmt.Errorf("failed to find mirror location in toml tree: %s", m)
			}

			apiIntMirror, err := i.newMirrorTree(location, i.iriRegistryDomain)
			if err != nil {
				return "", err
			}
			iriMirrors = append(iriMirrors, apiIntMirror)

			localHostMirror, err := i.newMirrorTree(location, "localhost")
			if err != nil {
				return "", err
			}
			iriMirrors = append(iriMirrors, localHostMirror)
		}

		// Update the current registry tree.
		registry.Set("mirror", append(mirrorTrees, iriMirrors...))
	}

	return registryTOML.String(), nil
}

func (i *internalReleaseImagePatcher) newMirrorTree(location string, mirror string) (*toml.Tree, error) {
	treeMap := map[string]interface{}{
		"location": i.patchMirror(location, mirror),
		"insecure": false,
	}
	m, err := toml.TreeFromMap(treeMap)
	if err != nil {
		return nil, err
	}
	return m, nil
}
