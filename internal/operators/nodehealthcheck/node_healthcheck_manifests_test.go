package nodehealthcheck

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gopkg.in/yaml.v3"
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
		operator = NewNodeHealthcheckOperator(common.GetTestLog())
	})

	It("Generates the required manifests", func() {
		manifests, _, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(manifests).To(HaveKey("50_node_healthcheck_namespace.yaml"))
		Expect(manifests).To(HaveKey("50_node_healthcheck_subscription.yaml"))
		Expect(manifests).To(HaveKey("50_node_healthcheck_operatorgroup.yaml"))
	})

	It("Generates valid YAML", func() {
		openShiftManifests, customManifest, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		for _, openShiftManifest := range openShiftManifests {
			var object any
			err = yaml.Unmarshal(openShiftManifest, &object)
			Expect(err).ToNot(HaveOccurred())
		}
		var object any
		err = yaml.Unmarshal(customManifest, &object)
		Expect(err).ToNot(HaveOccurred())
	})
})
