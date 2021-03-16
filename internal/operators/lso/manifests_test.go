package lso

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("LSO manifest generation", func() {
	operator := NewLSOperator()
	cluster := common.Cluster{Cluster: models.Cluster{
		OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
	}}

	Context("Create LSO Manifest", func() {

		manifests, err := operator.GenerateManifests(&cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(manifests).To(HaveLen(5))
		Expect(manifests["99_openshift-lso_ns.yaml"]).NotTo(HaveLen(0))
		Expect(manifests["99_openshift-lso_operator_group.yaml"]).NotTo(HaveLen(0))
		Expect(manifests["99_openshift-lso_subscription.yaml"]).NotTo(HaveLen(0))
		Expect(manifests["99_openshift-lso_lvset_cr.yaml"]).NotTo(HaveLen(0))
		Expect(manifests["99_openshift-lso_lvset_crd.yaml"]).NotTo(HaveLen(0))

		for _, manifest := range manifests {
			_, err := yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())
		}
	})
})
