package metallb_test

import (
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/metallb"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("MetalLB manifest generation", func() {
	var (
		cluster *common.Cluster
	)

	BeforeEach(func() {
		clusterID := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			Name:             "test-cluster",
			OpenshiftVersion: "4.11.0",
			BaseDNSDomain:    "example.com",
		}}
	})

	Context("Manifests function", func() {
		It("should generate valid manifests", func() {
			manifests, tgzManifests, err := metallb.Manifests(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(tgzManifests).ToNot(BeNil())
			Expect(len(tgzManifests)).To(BeNumerically(">", 0))

			// Verify we have the expected manifests
			var foundNamespace, foundOperatorGroup, foundSubscription bool
			for filename, content := range manifests {
				var obj map[string]interface{}
				err := yaml.Unmarshal(content, &obj)
				Expect(err).ToNot(HaveOccurred())

				kind := obj["kind"].(string)
				switch kind {
				case "Namespace":
					foundNamespace = true
					metadata := obj["metadata"].(map[string]interface{})
					Expect(metadata["name"]).To(Equal("metallb-system"))
				case "OperatorGroup":
					foundOperatorGroup = true
					metadata := obj["metadata"].(map[string]interface{})
					Expect(metadata["namespace"]).To(Equal("metallb-system"))
				case "Subscription":
					foundSubscription = true
					metadata := obj["metadata"].(map[string]interface{})
					Expect(metadata["name"]).To(Equal("metallb-operator"))
					Expect(metadata["namespace"]).To(Equal("metallb-system"))

					spec := obj["spec"].(map[string]interface{})
					Expect(spec["name"]).To(Equal("metallb-operator"))
					Expect(spec["source"]).To(Equal("redhat-operators"))
					Expect(spec["sourceNamespace"]).To(Equal("openshift-marketplace"))
				}

				Expect(filename).ToNot(BeEmpty())
				Expect(content).ToNot(BeEmpty())
			}

			Expect(foundNamespace).To(BeTrue(), "Namespace manifest should be generated")
			Expect(foundOperatorGroup).To(BeTrue(), "OperatorGroup manifest should be generated")
			Expect(foundSubscription).To(BeTrue(), "Subscription manifest should be generated")
		})

		It("should generate manifests with consistent content", func() {
			manifests1, _, err1 := metallb.Manifests(cluster)
			Expect(err1).ToNot(HaveOccurred())

			manifests2, _, err2 := metallb.Manifests(cluster)
			Expect(err2).ToNot(HaveOccurred())

			Expect(manifests1).To(Equal(manifests2))
		})
	})
})
