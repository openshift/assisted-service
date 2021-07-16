package cnv

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("CNV manifest generation", func() {
	operator := NewCNVOperator(common.GetTestLog(), Config{Mode: true}, nil)
	cluster := common.Cluster{Cluster: models.Cluster{
		OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
	}}

	Context("CNV Manifest", func() {

		It("Should create manifestes", func() {
			openshiftManifests, manifests, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["99_openshift-cnv_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["99_openshift-cnv_operator_group.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["99_openshift-cnv_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(manifests).To(HaveLen(1))
			Expect(manifests["99_openshift-cnv_hco.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range manifests {
				_, err := yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			for _, manifest := range openshiftManifests {
				_, err := yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}
		})

		It("Should create downstream manifests", func() {
			openshiftManifests, _, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(meta(openshiftManifests["99_openshift-cnv_ns.yaml"], "name")).To(Equal("openshift-cnv"))
			Expect(meta(openshiftManifests["99_openshift-cnv_subscription.yaml"], "namespace")).To(Equal("openshift-cnv"))
		})
	})
})

func meta(manifest []byte, param string) string {
	var data interface{}
	err := yaml.Unmarshal(manifest, &data)
	Expect(err).ShouldNot(HaveOccurred())
	ns := data.(map[string]interface{})
	meta := ns["metadata"]
	m := meta.(map[string]interface{})
	return m[param].(string)
}
