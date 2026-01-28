package ignition

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	mcfgv1alpha1 "github.com/openshift/api/machineconfiguration/v1alpha1"
	"github.com/openshift/assisted-service/internal/common"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/pelletier/go-toml"
	"github.com/sirupsen/logrus"
	"github.com/vincent-petithory/dataurl"
)

var _ = Describe("InternalReleaseImage resources patching", func() {
	var (
		mockS3Client *s3wrapper.MockAPI
		cluster      *common.Cluster
		ctrl         *gomock.Controller
		manifestsAPI *manifestsapi.MockManifestsAPI
		iriPatcher   internalReleaseImagePatcher
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		manifestsAPI = manifestsapi.NewMockManifestsAPI(ctrl)
		cluster = testCluster()
		cluster.Name = "ostest"
		cluster.BaseDNSDomain = "test.metalkube.org"

		iriPatcher = NewInternalReleaseImagePatcher(cluster, mockS3Client, manifestsAPI, logrus.New())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("when IRI resource was found", func() {
		It("add IRI mirrors to bootstrap.ign/registries.conf", func() {
			iriPatcher.iri = &mcfgv1alpha1.InternalReleaseImage{}
			bootstrapIgnition := iriBootstrapIgnition()

			err := iriPatcher.UpdateBootstrap(bootstrapIgnition)
			Expect(err).NotTo(HaveOccurred())

			actualRegistriesConf := getRegistriesConf(bootstrapIgnition)
			Expect(sameRegistriesConf(actualRegistriesConf, expectedIRIRegistriesConf)).To(BeTrue(), "Mismatch found in the patched registries.conf")
		})

		It("add IRI mirrors to IDMS/ITMS/CS/CC extra manifests", func() {
			extraManifests := iriSetupExtraManifests(mockS3Client)

			manifestsAPI.EXPECT().UpdateClusterManifestInternal(context.TODO(),
				ManifestContains("idms-oc-mirror.yaml", "api-int.ostest.test.metalkube.org:22625", "localhost:22625")).Return(nil, nil)
			manifestsAPI.EXPECT().UpdateClusterManifestInternal(context.TODO(),
				ManifestContains("itms-oc-mirror.yaml", "api-int.ostest.test.metalkube.org:22625", "localhost:22625")).Return(nil, nil)
			manifestsAPI.EXPECT().UpdateClusterManifestInternal(context.TODO(),
				ManifestContains("cc-redhat-operator-index.yaml", "api-int.ostest.test.metalkube.org:22625")).Return(nil, nil)
			manifestsAPI.EXPECT().UpdateClusterManifestInternal(context.TODO(),
				ManifestContains("cs-redhat-operator-index.yaml", "api-int.ostest.test.metalkube.org:22625")).Return(nil, nil)

			err := iriPatcher.PatchManifests(context.TODO(), extraManifests)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when IRI wasn't found", func() {
		It("do not update bootstrap.ign", func() {
			iriPatcher.iri = nil
			bootstrapIgnition := iriBootstrapIgnition()

			err := iriPatcher.UpdateBootstrap(bootstrapIgnition)
			Expect(err).NotTo(HaveOccurred())

			actualRegistriesConf := getRegistriesConf(bootstrapIgnition)
			Expect(sameRegistriesConf(actualRegistriesConf, applianceRegistriesConf)).To(BeTrue())
		})
	})
})

func ManifestContains(manifest string, s ...string) manifestContainsMatcher {
	return manifestContainsMatcher{
		manifest: manifest,
		expected: s,
	}
}

type manifestContainsMatcher struct {
	manifest string
	expected []string
}

func (m manifestContainsMatcher) Matches(x any) bool {
	params, ok := x.(operations.V2UpdateClusterManifestParams)
	if !ok {
		return false
	}
	if params.UpdateManifestParams.Folder != "openshift" {
		return false
	}
	if params.UpdateManifestParams.FileName != m.manifest {
		return false
	}
	data, err := base64.StdEncoding.DecodeString(*params.UpdateManifestParams.UpdatedContent)
	if err != nil {
		return false
	}
	for _, s := range m.expected {
		if !strings.Contains(string(data), s) {
			return false
		}
	}
	return true
}

func (m manifestContainsMatcher) String() string {
	return fmt.Sprintf("manifest contains %v", m.expected)
}

func getRegistriesConf(config *types.Config) string {
	var rc *types.File
	for _, f := range config.Storage.Files {
		if f.Path == registriesConfKey {
			rc = &f
			break
		}
	}
	Expect(rc).NotTo(BeNil())
	dataURL, err := dataurl.DecodeString(rc.FileEmbedded1.Contents.Key())
	Expect(err).NotTo(HaveOccurred())
	return string(dataURL.Data)
}

func s3ClientAdd(mockS3Client *s3wrapper.MockAPI, path string, data string) s3wrapper.ObjectInfo {
	mockS3Client.EXPECT().Download(context.TODO(), path).
		DoAndReturn(func(ctx context.Context, p string) (io.ReadCloser, int64, error) {
			return io.NopCloser(strings.NewReader(data)), int64(len(data)), nil
		}).
		AnyTimes()

	return s3wrapper.ObjectInfo{
		Path: path,
	}
}

func iriSetupExtraManifests(mockS3Client *s3wrapper.MockAPI) []s3wrapper.ObjectInfo {
	objs := []s3wrapper.ObjectInfo{}
	objs = append(objs, s3ClientAdd(mockS3Client, "/etc/assisted/extra-manifests/internalreleaseimage.yaml", manifestIRI))
	objs = append(objs, s3ClientAdd(mockS3Client, "/etc/assisted/extra-manifests/idms-oc-mirror.yaml", manifestIDMS))
	objs = append(objs, s3ClientAdd(mockS3Client, "/etc/assisted/extra-manifests/itms-oc-mirror.yaml", manifestITMS))
	objs = append(objs, s3ClientAdd(mockS3Client, "/etc/assisted/extra-manifests/cs-redhat-operator-index.yaml", manifestCatalogSource))
	objs = append(objs, s3ClientAdd(mockS3Client, "/etc/assisted/extra-manifests/cc-redhat-operator-index.yaml", manifestClusterCatalog))
	return objs
}

func ignEncodeStr(data string) string {
	return "data:;base64," + base64.StdEncoding.EncodeToString([]byte(data))
}

func iriBootstrapIgnition() *types.Config {
	return &types.Config{
		Storage: types.Storage{
			Files: []types.File{
				{
					Node: types.Node{
						Path: "/etc/containers/registries.conf",
					},
					FileEmbedded1: types.FileEmbedded1{
						Contents: types.Resource{
							Source: swag.String(ignEncodeStr(applianceRegistriesConf)),
						},
					},
				},
			},
		},
	}
}

func sameRegistriesConf(actualRC, expectedRC string) bool {
	t1, err := toml.Load(actualRC)
	if err != nil {
		return false
	}
	t2, err := toml.Load(expectedRC)
	if err != nil {
		return false
	}
	m1 := t1.ToMap()
	m2 := t2.ToMap()

	j1, err := json.Marshal(m1)
	if err != nil {
		return false
	}
	j2, err := json.Marshal(m2)
	if err != nil {
		return false
	}
	return bytes.Equal(j1, j2)
}

var applianceRegistriesConf = `
[[registry]]
location = "quay.io/openshift-release-dev/ocp-v4.0-art-dev"
insecure = false
mirror-by-digest-only = true
blocked = false

[[registry.mirror]]
location = "registry.appliance.openshift.com:22625/openshift/release"
insecure = false

[[registry]]
location = "registry.ci.openshift.org/ocp/release"
insecure = false
mirror-by-digest-only = true
blocked = false

[[registry.mirror]]
location = "registry.appliance.openshift.com:22625/openshift/release-images"
insecure = false

[[registry]]
location = "registry.redhat.io/rhel9"
insecure = false
mirror-by-digest-only = true
blocked = false

[[registry.mirror]]
location = "registry.appliance.openshift.com:22625/rhel9"
insecure = false`

var expectedIRIRegistriesConf = `
[[registry]]
location = "quay.io/openshift-release-dev/ocp-v4.0-art-dev"
insecure = false
mirror-by-digest-only = true
blocked = false

[[registry.mirror]]
location = "registry.appliance.openshift.com:22625/openshift/release"
insecure = false
[[registry.mirror]]
location = "api-int.ostest.test.metalkube.org:22625/openshift/release"
insecure = false
[[registry.mirror]]
location = "localhost:22625/openshift/release"
insecure = false

[[registry]]
location = "registry.ci.openshift.org/ocp/release"
insecure = false
mirror-by-digest-only = true
blocked = false

[[registry.mirror]]
location = "registry.appliance.openshift.com:22625/openshift/release-images"
insecure = false
[[registry.mirror]]
location = "api-int.ostest.test.metalkube.org:22625/openshift/release-images"
insecure = false
[[registry.mirror]]
location = "localhost:22625/openshift/release-images"
insecure = false

[[registry]]
location = "registry.redhat.io/rhel9"
insecure = false
mirror-by-digest-only = true
blocked = false

[[registry.mirror]]
location = "registry.appliance.openshift.com:22625/rhel9"
insecure = false
[[registry.mirror]]
location = "api-int.ostest.test.metalkube.org:22625/rhel9"
insecure = false
[[registry.mirror]]
location = "localhost:22625/rhel9"
insecure = false`

var manifestIRI = `
kind: InternalReleaseImage
metadata:
  name: cluster
spec:
  releases:
  - name: ocp-release-bundle-4.21.0-0.nightly-2025-12-14-144544
`

var manifestIDMS = `
apiVersion: config.openshift.io/v1
kind: ImageDigestMirrorSet
metadata:
  name: idms-release-0
spec:
  imageDigestMirrors:
  - mirrors:
    - registry.appliance.com:5000/openshift/release
    source: quay.io/openshift-release-dev/ocp-v4.0-art-dev
  - mirrors:
    - registry.appliance.com:5000/openshift/release-images
    source: quay.io/openshift-release-dev/ocp-release
`

var manifestITMS = `
apiVersion: config.openshift.io/v1
kind: ImageTagMirrorSet
metadata:
  name: itms-generic-0
spec:
  imageTagMirrors:
  - mirrors:
    - registry.appliance.com:5000/rhel9
    source: registry.redhat.io/rhel9
`

var manifestClusterCatalog = `
apiVersion: olm.operatorframework.io/v1
kind: ClusterCatalog
metadata:
  name: cc-redhat-operator-index
spec:
  priority: 0
  source:
    image:
      ref: registry.appliance.com:5000/redhat/redhat-operator-index:v4.19
    type: Image
`

var manifestCatalogSource = `
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: cs-redhat-operator-index
  namespace: openshift-marketplace
spec:
  image: registry.appliance.com:5000/redhat/redhat-operator-index:v4.19
  sourceType: grpc
`
