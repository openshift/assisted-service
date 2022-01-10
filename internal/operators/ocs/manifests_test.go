package ocs

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("OCS manifest generation", func() {
	operator := NewOcsOperator(common.GetTestLog(), nil)
	operator.config.OCSDeploymentType = "Compact"
	cluster := common.Cluster{Cluster: models.Cluster{
		OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
	}}

	Context("Create OCS Manifests for all deployment modes", func() {
		It("Check YAMLs of OCS in Compact Mode", func() {
			operator.config.OCSDeploymentType = "Compact"
			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["openshift-ocs_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["openshift-ocs_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["openshift-ocs_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Check YAMLs of OCS in Standard Mode", func() {
			operator.config.OCSDeploymentType = "Standard"
			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["openshift-ocs_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["openshift-ocs_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["openshift-ocs_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())
		})

	})
})
