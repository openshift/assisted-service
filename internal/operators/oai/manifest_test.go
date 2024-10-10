package oai

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("Manifest generation", func() {
	var (
		cluster  *common.Cluster
		operator *operator
	)

	BeforeEach(func() {
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				OpenshiftVersion: "4.12.0",
			},
		}
		operator = NewOperator(common.GetTestLog())
	})

	It("Generates the required manifests", func() {
		manifests, _, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(manifests).To(HaveKey("50_openshift-ai_ns.yaml"))
		Expect(manifests).To(HaveKey("50_openshift-ai_operator_subscription.yaml"))
		Expect(manifests).To(HaveKey("50_openshift-ai_operator_group.yaml"))
	})

	It("Generates valid YAML", func() {
		openShiftManifests, customManifest, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		for _, openShiftManifest := range openShiftManifests {
			_, err = yaml.YAMLToJSON(openShiftManifest)
			Expect(err).ToNot(HaveOccurred())
		}
		_, err = yaml.YAMLToJSON(customManifest)
		Expect(err).ToNot(HaveOccurred())
	})
})
