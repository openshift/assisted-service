package mce

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("MCE manifest generation", func() {
	operator := NewMceOperator(common.GetTestLog())
	var cluster *common.Cluster

	getCluster := func(openshiftVersion string) *common.Cluster {
		cluster := common.Cluster{Cluster: models.Cluster{
			OpenshiftVersion: openshiftVersion,
		}}
		return &cluster
	}

	Context("MCE Manifest", func() {
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
