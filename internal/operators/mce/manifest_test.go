package mce

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("MCE manifest generation", func() {

	config := Config{
		OcpMceVersionMap: []OcpMceVersionMap{
			{
				OpenshiftVersion: "4.11",
				MceChannel:       "stable-2.3",
			},
			{
				OpenshiftVersion: "4.12",
				MceChannel:       "stable-2.4",
			},
			{
				OpenshiftVersion: "4.13",
				MceChannel:       "stable-2.4",
			},
			{
				OpenshiftVersion: "4.14",
				MceChannel:       "stable-2.4",
			},
			{
				OpenshiftVersion: "4.15",
				MceChannel:       "stable-2.4",
			},
		},
	}

	operator := NewMceOperator(common.GetTestLog(), EnvironmentalConfig{})
	var cluster *common.Cluster

	getCluster := func(openshiftVersion string) *common.Cluster {
		cluster := common.Cluster{Cluster: models.Cluster{
			OpenshiftVersion: openshiftVersion,
		}}
		return &cluster
	}

	Context("MCE Manifest", func() {
		It("Get MCE channel", func() {
			var (
				version *string
				err     error
			)

			version, err = getMCEVersion("4.15", config.OcpMceVersionMap)
			Expect(err).ToNot(HaveOccurred())
			Expect(*version).To(Equal("stable-2.4"))

			version, err = getMCEVersion("4.14", config.OcpMceVersionMap)
			Expect(err).ToNot(HaveOccurred())
			Expect(*version).To(Equal("stable-2.4"))

			version, err = getMCEVersion("4.13", config.OcpMceVersionMap)
			Expect(err).ToNot(HaveOccurred())
			Expect(*version).To(Equal("stable-2.4"))

			version, err = getMCEVersion("4.12", config.OcpMceVersionMap)
			Expect(err).ToNot(HaveOccurred())
			Expect(*version).To(Equal("stable-2.4"))

			version, err = getMCEVersion("4.11", config.OcpMceVersionMap)
			Expect(err).ToNot(HaveOccurred())
			Expect(*version).To(Equal("stable-2.3"))

			_, err = getMCEVersion("4.10", config.OcpMceVersionMap)
			Expect(err).To(HaveOccurred())

			version, err = getMCEVersion("4.12.0-0.nightly-2022-10-25-210451", config.OcpMceVersionMap)
			Expect(err).ToNot(HaveOccurred())
			Expect(*version).To(Equal("stable-2.4"))

			version, err = getMCEVersion("4.11.0-ec.3", config.OcpMceVersionMap)
			Expect(err).ToNot(HaveOccurred())
			Expect(*version).To(Equal("stable-2.3"))
		})

		It("Check YAMLs of MCE", func() {
			cluster = getCluster("4.11.0")
			openshiftManifests, manifest, err := operator.GenerateManifests(cluster)

			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-mce_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-mce_operator_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-mce_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred(), "yamltojson err: %v", err)
		})
	})
})
