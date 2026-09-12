package ignition

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"

	types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pelletier/go-toml"
	"github.com/sirupsen/logrus"
	"github.com/vincent-petithory/dataurl"
	"go.uber.org/mock/gomock"
	"sigs.k8s.io/yaml"
)

var _ = Describe("mirrorHasLocalhost", func() {
	It("returns true for localhost with no path", func() {
		Expect(mirrorHasLocalhost(configv1.ImageMirror("localhost"))).To(BeTrue())
	})
	It("returns true for localhost with port", func() {
		Expect(mirrorHasLocalhost(configv1.ImageMirror("localhost:22625"))).To(BeTrue())
	})
	It("returns true for localhost with port and path", func() {
		Expect(mirrorHasLocalhost(configv1.ImageMirror("localhost:22625/openshift/release"))).To(BeTrue())
	})
	It("returns false for non-localhost host", func() {
		Expect(mirrorHasLocalhost(configv1.ImageMirror("registry.appliance.com:5000/openshift/release"))).To(BeFalse())
	})
	It("returns false for api-int host", func() {
		Expect(mirrorHasLocalhost(configv1.ImageMirror("api-int.ostest.test.metalkube.org:22625/openshift/release"))).To(BeFalse())
	})
})

var _ = Describe("mirrorsContainLocalhost", func() {
	It("returns false for empty slice", func() {
		Expect(mirrorsContainLocalhost(nil)).To(BeFalse())
		Expect(mirrorsContainLocalhost([]configv1.ImageMirror{})).To(BeFalse())
	})
	It("returns false when no mirror uses localhost", func() {
		mirrors := []configv1.ImageMirror{
			"registry.appliance.com:5000/openshift/release",
			"api-int.ostest.test.metalkube.org:22625/openshift/release",
		}
		Expect(mirrorsContainLocalhost(mirrors)).To(BeFalse())
	})
	It("returns true when one mirror uses localhost", func() {
		mirrors := []configv1.ImageMirror{
			"registry.appliance.com:5000/openshift/release",
			"localhost:22625/openshift/release",
		}
		Expect(mirrorsContainLocalhost(mirrors)).To(BeTrue())
	})
	It("returns true when only mirror is localhost", func() {
		mirrors := []configv1.ImageMirror{"localhost:22625/openshift/release"}
		Expect(mirrorsContainLocalhost(mirrors)).To(BeTrue())
	})
})

var _ = Describe("InternalReleaseImage resources patching", func() {
	var (
		mockS3Client *s3wrapper.MockAPI
		cluster      *common.Cluster
		ctrl         *gomock.Controller
		iriPatcher   internalReleaseImagePatcher
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		cluster = testCluster()
		cluster.Name = "ostest"
		cluster.BaseDNSDomain = "test.metalkube.org"

		iriPatcher = NewInternalReleaseImagePatcher(cluster, mockS3Client, logrus.New())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("when IRI resource was found", func() {
		It("add IRI mirrors to bootstrap.ign/registries.conf", func() {
			iriPatcher.iriFound = true
			bootstrapIgnition := iriBootstrapIgnition()

			err := iriPatcher.UpdateBootstrap(bootstrapIgnition)
			Expect(err).NotTo(HaveOccurred())

			actualRegistriesConf := getRegistriesConf(bootstrapIgnition)
			Expect(sameRegistriesConf(actualRegistriesConf, expectedIRIRegistriesConf)).To(BeTrue(), "Mismatch found in the patched registries.conf")
		})

		It("add IRI mirrors to IDMS/ITMS/CS/CC extra manifests", func() {
			extraManifests := iriSetupExtraManifests(mockS3Client)

			mockS3Client.EXPECT().UploadWithMetadata(context.TODO(),
				UploadedContentContains("api-int.ostest.test.metalkube.org:22625", "localhost:22625"),
				MatchSubstring("idms-oc-mirror.yaml"), SystemManifestMetadata()).Return(nil)
			mockS3Client.EXPECT().UploadWithMetadata(context.TODO(),
				UploadedContentContains("api-int.ostest.test.metalkube.org:22625", "localhost:22625"),
				MatchSubstring("itms-oc-mirror.yaml"), SystemManifestMetadata()).Return(nil)
			mockS3Client.EXPECT().UploadWithMetadata(context.TODO(),
				UploadedContentContains("api-int.ostest.test.metalkube.org:22625"),
				MatchSubstring("cc-redhat-operator-index.yaml"), SystemManifestMetadata()).Return(nil)
			mockS3Client.EXPECT().UploadWithMetadata(context.TODO(),
				UploadedContentContains("api-int.ostest.test.metalkube.org:22625"),
				MatchSubstring("cs-redhat-operator-index.yaml"), SystemManifestMetadata()).Return(nil)
			// internalreleaseimage.yaml is also marked as system
			mockS3Client.EXPECT().UploadWithMetadata(context.TODO(),
				gomock.Any(),
				MatchSubstring("internalreleaseimage.yaml"), SystemManifestMetadata()).Return(nil)

			err := iriPatcher.PatchManifests(context.TODO(), extraManifests)
			Expect(err).NotTo(HaveOccurred())
		})

		It("do not add duplicate localhost to IDMS/ITMS when mirrors already contain localhost", func() {
			extraManifests := iriSetupExtraManifestsWithLocalhost(mockS3Client)
			var idmsContent, itmsContent []byte

			mockS3Client.EXPECT().UploadWithMetadata(context.TODO(), gomock.Any(), gomock.Any(), SystemManifestMetadata()).
				DoAndReturn(func(ctx context.Context, data []byte, objectName string, metadata map[string]string) error {
					if strings.Contains(objectName, "idms-oc-mirror.yaml") {
						idmsContent = data
					} else if strings.Contains(objectName, "itms-oc-mirror.yaml") {
						itmsContent = data
					}
					return nil
				}).Times(5) // 4 patched manifests + internalreleaseimage.yaml

			err := iriPatcher.PatchManifests(context.TODO(), extraManifests)
			Expect(err).NotTo(HaveOccurred())

			Expect(idmsContent).NotTo(BeNil())
			Expect(idmsLocalhostMirrorCountFromBytes(idmsContent)).To(Equal(1), "IDMS should have exactly one localhost mirror when one was already present (no duplicate)")
			Expect(string(idmsContent)).To(ContainSubstring("api-int.ostest.test.metalkube.org:22625"))

			Expect(itmsContent).NotTo(BeNil())
			Expect(itmsLocalhostMirrorCountFromBytes(itmsContent)).To(Equal(1), "ITMS should have exactly one localhost mirror when one was already present (no duplicate)")
			Expect(string(itmsContent)).To(ContainSubstring("api-int.ostest.test.metalkube.org:22625"))
		})
	})

	Context("when IRI wasn't found", func() {
		It("do not update bootstrap.ign", func() {
			iriPatcher.iriFound = false
			bootstrapIgnition := iriBootstrapIgnition()

			err := iriPatcher.UpdateBootstrap(bootstrapIgnition)
			Expect(err).NotTo(HaveOccurred())

			actualRegistriesConf := getRegistriesConf(bootstrapIgnition)
			Expect(sameRegistriesConf(actualRegistriesConf, applianceRegistriesConf)).To(BeTrue())
		})
	})
})

// UploadedContentContains returns a gomock matcher that checks if the uploaded byte content
// contains all expected substrings.
func UploadedContentContains(expected ...string) gomock.Matcher {
	return uploadedContentContainsMatcher{expected: expected}
}

type uploadedContentContainsMatcher struct {
	expected []string
}

func (m uploadedContentContainsMatcher) Matches(x any) bool {
	data, ok := x.([]byte)
	if !ok {
		return false
	}
	s := string(data)
	for _, exp := range m.expected {
		if !strings.Contains(s, exp) {
			return false
		}
	}
	return true
}

func (m uploadedContentContainsMatcher) String() string {
	return "uploaded content contains " + strings.Join(m.expected, ", ")
}

// SystemManifestMetadata returns a gomock matcher that checks for system manifest metadata.
func SystemManifestMetadata() gomock.Matcher {
	return systemManifestMetadataMatcher{}
}

type systemManifestMetadataMatcher struct{}

func (m systemManifestMetadataMatcher) Matches(x any) bool {
	metadata, ok := x.(map[string]string)
	if !ok {
		return false
	}
	return metadata[constants.ManifestSourceAttribute] == constants.ManifestSourceSystemGenerated
}

func (m systemManifestMetadataMatcher) String() string {
	return "metadata with system manifest source"
}

// MatchSubstring returns a gomock matcher for string containment.
func MatchSubstring(substr string) gomock.Matcher {
	return containSubstringMatcher{substr: substr}
}

type containSubstringMatcher struct {
	substr string
}

func (m containSubstringMatcher) Matches(x any) bool {
	s, ok := x.(string)
	if !ok {
		return false
	}
	return strings.Contains(s, m.substr)
}

func (m containSubstringMatcher) String() string {
	return "contains substring " + m.substr
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

func idmsLocalhostMirrorCountFromBytes(content []byte) int {
	var idms configv1.ImageDigestMirrorSet
	Expect(yaml.Unmarshal(content, &idms)).NotTo(HaveOccurred())
	count := 0
	for _, group := range idms.Spec.ImageDigestMirrors {
		for _, m := range group.Mirrors {
			if mirrorHasLocalhost(m) {
				count++
			}
		}
	}
	return count
}

func itmsLocalhostMirrorCountFromBytes(content []byte) int {
	var itms configv1.ImageTagMirrorSet
	Expect(yaml.Unmarshal(content, &itms)).NotTo(HaveOccurred())
	count := 0
	for _, group := range itms.Spec.ImageTagMirrors {
		for _, m := range group.Mirrors {
			if mirrorHasLocalhost(m) {
				count++
			}
		}
	}
	return count
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

func iriSetupExtraManifestsWithLocalhost(mockS3Client *s3wrapper.MockAPI) []s3wrapper.ObjectInfo {
	objs := []s3wrapper.ObjectInfo{}
	objs = append(objs, s3ClientAdd(mockS3Client, "/etc/assisted/extra-manifests/internalreleaseimage.yaml", manifestIRI))
	objs = append(objs, s3ClientAdd(mockS3Client, "/etc/assisted/extra-manifests/idms-oc-mirror.yaml", manifestIDMSWithLocalhost))
	objs = append(objs, s3ClientAdd(mockS3Client, "/etc/assisted/extra-manifests/itms-oc-mirror.yaml", manifestITMSWithLocalhost))
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
apiVersion: machineconfiguration.openshift.io/v1
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

// manifestIDMSWithLocalhost has one mirror group that already contains localhost (e.g. from appliance).
// Patcher should add api-int but not duplicate localhost.
var manifestIDMSWithLocalhost = `
apiVersion: config.openshift.io/v1
kind: ImageDigestMirrorSet
metadata:
  name: idms-release-0
spec:
  imageDigestMirrors:
  - mirrors:
    - registry.appliance.com:5000/openshift/release
    - localhost:22625/openshift/release
    source: quay.io/openshift-release-dev/ocp-v4.0-art-dev
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

// manifestITMSWithLocalhost has mirrors that already contain localhost (e.g. from appliance).
// Patcher should add api-int but not duplicate localhost.
var manifestITMSWithLocalhost = `
apiVersion: config.openshift.io/v1
kind: ImageTagMirrorSet
metadata:
  name: itms-generic-0
spec:
  imageTagMirrors:
  - mirrors:
    - registry.appliance.com:5000/rhel9
    - localhost:22625/rhel9
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
