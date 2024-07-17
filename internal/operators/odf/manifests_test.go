package odf

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("OCS manifest generation", func() {
	operator := NewOdfOperator(common.GetTestLog())

	Context("Create OCS Manifests for all deployment modes with openshiftVersion as 4.8.X", func() {
		It("Check YAMLs of OCS in Compact Mode", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.8.0",
			}}
			operator.config.ODFDeploymentType = "Compact"
			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-ocs_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-ocs_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-ocs_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Check YAMLs of OCS in Standard Mode", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.8.0",
			}}
			operator.config.ODFDeploymentType = "Standard"
			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-ocs_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-ocs_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-ocs_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())
		})

	})

	Context("Create ODF Manifests for all deployment modes with openshiftVersion as 4.9.X or above", func() {
		It("Check YAMLs of ODF in Compact Mode", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.9.0",
			}}
			operator.config.ODFDeploymentType = "Compact"
			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-odf_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-odf_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-odf_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Check YAMLs of ODF in Standard Mode", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.9.0",
			}}
			operator.config.ODFDeploymentType = "Standard"
			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-odf_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-odf_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-odf_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())
		})

	})
})
