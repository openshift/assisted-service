package lso

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	models "github.com/openshift/assisted-service/models/v1"
	"sigs.k8s.io/yaml"
)

var _ = Describe("LSO manifest generation", func() {
	operator := NewLSOperator()
	cluster := common.Cluster{Cluster: models.Cluster{
		OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
	}}

	Context("Create LSO Manifest", func() {

		openshiftManifests, manifests, err := operator.GenerateManifests(&cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(openshiftManifests).To(HaveLen(3))
		Expect(openshiftManifests["99_openshift-lso_ns.yaml"]).NotTo(HaveLen(0))
		Expect(openshiftManifests["99_openshift-lso_operator_group.yaml"]).NotTo(HaveLen(0))
		Expect(openshiftManifests["99_openshift-lso_subscription.yaml"]).NotTo(HaveLen(0))

		Expect(manifests).To(HaveLen(1))
		Expect(manifests["99_openshift-lso_lvset_cr.yaml"]).NotTo(HaveLen(0))

		for _, manifest := range openshiftManifests {
			_, err := yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())
		}

		for _, manifest := range manifests {
			_, err := yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())
		}
	})
})
