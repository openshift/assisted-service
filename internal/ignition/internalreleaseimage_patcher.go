package ignition

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"path"
	"regexp"

	config_latest_types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/go-openapi/swag"
	configv1 "github.com/openshift/api/config/v1"
	mcfgv1alpha1 "github.com/openshift/api/machineconfiguration/v1alpha1"
	"github.com/openshift/assisted-service/internal/common"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/pelletier/go-toml"
	"github.com/sirupsen/logrus"
	"github.com/vincent-petithory/dataurl"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"
	k8syaml "sigs.k8s.io/yaml"
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
	manifestApi       manifestsapi.ManifestsAPI
	iriRegistryDomain string
	iri               *mcfgv1alpha1.InternalReleaseImage
}

// NewInternalReleaseImagePatcher creates a new internalReleaseImagePatcher instance.
func NewInternalReleaseImagePatcher(cluster *common.Cluster, s3Client s3wrapper.API, manifestApi manifestsapi.ManifestsAPI, log logrus.FieldLogger) internalReleaseImagePatcher {
	return internalReleaseImagePatcher{
		cluster:           cluster,
		s3Client:          s3Client,
		manifestApi:       manifestApi,
		log:               log,
		iriRegistryDomain: fmt.Sprintf("api-int.%s.%s", cluster.Name, cluster.BaseDNSDomain),
		iri:               nil,
	}
}

func (i *internalReleaseImagePatcher) patchMirror(origMirror string, host string) string {
	re := regexp.MustCompile(`^[^/]+`)
	return re.ReplaceAllString(origMirror, fmt.Sprintf("%s:%d", host, iriRegistryPort))
}

func (i *internalReleaseImagePatcher) patchImageMirror(mirror configv1.ImageMirror, host string) configv1.ImageMirror {
	return configv1.ImageMirror(i.patchMirror(string(mirror), host))
}

func (i *internalReleaseImagePatcher) uploadManifests(ctx context.Context, key string, obj interface{}) error {
	// manifest has full path as object-key on s3: clusterID/manifests/[manifests|openshift]/filename
	fileName := path.Base(key)
	i.log.Infof("Updating resource %s as %s", key, fileName)

	data, err := k8syaml.Marshal(obj)
	if err != nil {
		return err
	}

	params := operations.V2UpdateClusterManifestParams{
		ClusterID: *i.cluster.ID,
		UpdateManifestParams: &models.UpdateManifestParams{
			FileName:       fileName,
			Folder:         models.UpdateManifestParamsFolderOpenshift,
			UpdatedContent: swag.String(base64.StdEncoding.EncodeToString(data)),
		},
	}
	_, err = i.manifestApi.UpdateClusterManifestInternal(ctx, params)
	if err != nil {
		return err
	}
	return nil
}

func (i *internalReleaseImagePatcher) getManifestContent(ctx context.Context, manifest string) ([]byte, error) {
	respBody, _, err := i.s3Client.Download(ctx, manifest)
	if err != nil {
		return nil, err
	}
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
		obj := &mcfgv1alpha1.InternalReleaseImage{}
		err = yaml.Unmarshal(content, obj)
		if err != nil {
			i.log.Debugf("Cannot decode manifest %s, skipping", f.Path)
			continue
		}

		if obj.Kind == iriKind && obj.Name == iriInstanceName {
			i.iri = obj.DeepCopy()
			break
		}
	}

	return nil
}

// PatchManifests looks if the InternalReleaseImage manifest has been defined. In such case, it extends the
// IDMS/ITMS manifests (generated by oc mirror) with additional mirror entries for localhost/api-int.
// ClusterCatalog/CatalogSources resources are instead patched to consume api-int.
func (i *internalReleaseImagePatcher) PatchManifests(ctx context.Context, manifestFiles []s3wrapper.ObjectInfo) error {
	i.log.Infof("Looking for InternalReleaseImage mirror resources")

	// Check if the InternalReleaseImage manifest exists.
	err := i.getInternalReleaseImageManifest(ctx, manifestFiles)
	if err != nil {
		return err
	}
	// Skip if InternalReleaseImage manifest wasn't found.
	if i.iri == nil {
		return nil
	}
	i.log.Infof("Patching InternalReleaseImage mirror resources")

	// Process the oc-mirror manifests.
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
		case "ImageDigestMirrorSet":
			i.log.Infof("Patching ImageDigestMirrorSet manifest %s", f.Path)
			var idms configv1.ImageDigestMirrorSet
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &idms); err != nil {
				return err
			}
			i.markAsPatched(&idms)
			for j, group := range idms.Spec.ImageDigestMirrors {
				iriMirrors := []configv1.ImageMirror{}
				// For every mirror found, let's add another one for api-int and localhost.
				for _, m := range group.Mirrors {
					iriMirrors = append(iriMirrors, i.patchImageMirror(m, i.iriRegistryDomain))
					iriMirrors = append(iriMirrors, i.patchImageMirror(m, "localhost"))
				}
				idms.Spec.ImageDigestMirrors[j].Mirrors = append(idms.Spec.ImageDigestMirrors[j].Mirrors, iriMirrors...)
			}
			err := i.uploadManifests(ctx, f.Path, &idms)
			if err != nil {
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
				// For every mirror found, let's add another one for api-int and localhost.
				for _, m := range group.Mirrors {
					iriMirrors = append(iriMirrors, i.patchImageMirror(m, i.iriRegistryDomain))
					iriMirrors = append(iriMirrors, i.patchImageMirror(m, "localhost"))
				}
				itms.Spec.ImageTagMirrors[j].Mirrors = append(itms.Spec.ImageTagMirrors[j].Mirrors, iriMirrors...)
			}
			err := i.uploadManifests(ctx, f.Path, &itms)
			if err != nil {
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
			err = unstructured.SetNestedField(cc.Object, newRef, "spec", "source", "image", "ref")
			if err != nil {
				return fmt.Errorf("error while decoding ClusterCatalog resource %s: %v", cc.GetName(), err)
			}
			err = i.uploadManifests(ctx, f.Path, cc.Object)
			if err != nil {
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
			err = unstructured.SetNestedField(cs.Object, newImage, "spec", "image")
			if err != nil {
				return fmt.Errorf("error while decoding CatalogSource resource %s: %v", cs.GetName(), err)
			}
			err = i.uploadManifests(ctx, f.Path, cs.Object)
			if err != nil {
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
	if i.iri == nil {
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
