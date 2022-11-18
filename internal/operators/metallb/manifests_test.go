package metallb

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("MetalLB manifest generation", func() {
	noneHighAvailabilityMode := models.ClusterHighAvailabilityModeNone
	operator := NewLvmOperator(common.GetTestLog(), nil)

	Context("MetalLB Manifest", func() {
		It("Check YAMLs of MetalLB in SNO deployment mode", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:     "4.10.17",
				HighAvailabilityMode: &noneHighAvailabilityMode,
			}}
			Expect(common.IsSingleNodeCluster(&cluster)).To(BeTrue())
			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-metallb_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-metallb_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-metallb_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred(), "yamltojson err: %v", err)
		})

	})
})
