package ocs_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("OCS manifest generation", func() {
	operator := ocs.NewOcsOperator(common.GetTestLog())
	cluster := common.Cluster{Cluster: models.Cluster{
		OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
	}}

	Context("Create OCS Manifest", func() {

		manifests, err := operator.GenerateManifests(&cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(manifests).To(HaveLen(5))
		Expect(manifests["99_openshift-ocssc.yaml"]).NotTo(HaveLen(0))
		Expect(manifests["99_openshift-ocssc_crd.yaml"]).NotTo(HaveLen(0))
		Expect(manifests["99_openshift-ocs_ns.yaml"]).NotTo(HaveLen(0))
		Expect(manifests["99_openshift-ocs_subscription.yaml"]).NotTo(HaveLen(0))
		Expect(manifests["99_openshift-ocs_operator_group.yaml"]).NotTo(HaveLen(0))

		for _, manifest := range manifests {
			_, err := yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())
		}
	})
})
