package numaresources

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("NUMA Resources manifests", func() {
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
		operator = NewNumaResourcesOperator(common.GetTestLog())
	})

	It("check that openshift manifests are created", func() {
		openshiftManifests, customManifests, err := operator.GenerateManifests(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(openshiftManifests).To(HaveLen(5))
		Expect(openshiftManifests).To(HaveKey("50_numaresources_namespace.yaml"))
		Expect(openshiftManifests).To(HaveKey("50_numaresources_operatorgroup.yaml"))
		Expect(openshiftManifests).To(HaveKey("50_numaresources_subscription.yaml"))
		Expect(openshiftManifests).To(HaveKey("50_numaresources_prometheus-role.yaml"))
		Expect(openshiftManifests).To(HaveKey("50_numaresources_prometheus-rolebinding.yaml"))
		Expect(customManifests).To(ContainSubstring("numaresources"))
	})
})
