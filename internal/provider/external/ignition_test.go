package external

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var (
	cluster *common.Cluster
	log     = logrus.New()
	workDir string
)

var _ = BeforeEach(func() {
	// setup temp workdir
	var err error
	workDir, err = os.MkdirTemp("", "assisted-install-test-")
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterEach(func() {
	os.RemoveAll(workDir)
})

var _ = Describe("Infrastructure CR", func() {
	When("platform is OCI", func() {
		var (
			provider provider.Provider
		)

		BeforeEach(func() {
			provider = NewExternalProvider(log, models.PlatformTypeOci)
		})

		It("should be patched", func() {
			base := `apiVersion: config.openshift.io/v1
kind: Infrastructure
metadata:
  creationTimestamp: "2023-06-19T13:49:07Z"
  generation: 1
  name: cluster
  resourceVersion: "553"
  uid: 240dc176-566e-4471-b9db-fb25c676ba33
spec:
  cloudConfig:
    name: ""
  platformSpec:
    type: None
status:
  apiServerInternalURI: https://api-int.test-infra-cluster-97ef21c5.assisted-ci.oci-rhelcert.edge-sro.rhecoeng.com:6443
  apiServerURL: https://api.test-infra-cluster-97ef21c5.assisted-ci.oci-rhelcert.edge-sro.rhecoeng.com:6443
  controlPlaneTopology: HighlyAvailable
  cpuPartitioning: None
  etcdDiscoveryDomain: ""
  infrastructureName: test-infra-cluster-97-w6b42
  infrastructureTopology: HighlyAvailable
  platform: None
  platformStatus:
    type: None
`

			expected := `apiVersion: config.openshift.io/v1
kind: Infrastructure
metadata:
  creationTimestamp: "2023-06-19T13:49:07Z"
  generation: 1
  name: cluster
  resourceVersion: "553"
  uid: 240dc176-566e-4471-b9db-fb25c676ba33
spec:
  cloudConfig:
    name: ""
  platformSpec:
    external:
      platformName: oci
    type: External
status:
  apiServerInternalURI: https://api-int.test-infra-cluster-97ef21c5.assisted-ci.oci-rhelcert.edge-sro.rhecoeng.com:6443
  apiServerURL: https://api.test-infra-cluster-97ef21c5.assisted-ci.oci-rhelcert.edge-sro.rhecoeng.com:6443
  controlPlaneTopology: HighlyAvailable
  cpuPartitioning: None
  etcdDiscoveryDomain: ""
  infrastructureName: test-infra-cluster-97-w6b42
  infrastructureTopology: HighlyAvailable
  platform: External
  platformStatus:
    external:
      cloudControllerManager:
        state: External
    type: External
`

			manifestsDir := filepath.Join(workDir, "manifests")
			Expect(os.Mkdir(manifestsDir, 0755)).To(Succeed())

			err := os.WriteFile(filepath.Join(manifestsDir, "cluster-infrastructure-02-config.yml"), []byte(base), 0600)
			Expect(err).NotTo(HaveOccurred())

			Expect(provider.PostCreateManifestsHook(cluster, nil, workDir)).To(Succeed())

			content, err := os.ReadFile(filepath.Join(manifestsDir, "cluster-infrastructure-02-config.yml"))
			Expect(err).NotTo(HaveOccurred())

			Expect(string(content)).To(Equal(expected))
		})
	})
})
