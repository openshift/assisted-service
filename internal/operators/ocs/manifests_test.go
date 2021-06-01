package ocs

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("OCS manifest generation", func() {
	operator := NewOcsOperator(common.GetTestLog())
	operator.config.OCSDeploymentType = "Compact"
	cluster := common.Cluster{Cluster: models.Cluster{
		OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
	}}

	Context("Create OCS Manifests for all deployment modes", func() {
		It("Check YAMLs of OCS in Compact Mode", func() {
			operator.config.OCSDeploymentType = "Compact"
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
		It("Check YAMLs of OCS in Minimal Mode", func() {
			operator.config.OCSDeploymentType = "Minimal"
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
		It("Check YAMLs of OCS in Standard Mode", func() {
			operator.config.OCSDeploymentType = "Standard"
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
})
