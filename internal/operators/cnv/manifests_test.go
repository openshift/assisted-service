package cnv

import (
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("CNV manifest generation", func() {
	fullHaMode := models.ClusterHighAvailabilityModeFull
	noneHaMode := models.ClusterHighAvailabilityModeNone
	operator := NewCNVOperator(common.GetTestLog(), Config{Mode: true, SNOInstallHPP: true}, nil)

	Context("CNV Manifest", func() {
		table.DescribeTable("Should create manifestes", func(cluster common.Cluster, isSno bool, cfg Config) {
			cnvOperator := NewCNVOperator(common.GetTestLog(), cfg, nil)
			openshiftManifests, manifest, err := cnvOperator.GenerateManifests(&cluster)
			numManifests := 3
			if isSno && cfg.SNOInstallHPP {
				// Add Hostpathprovisioner StorageClass to expectation
				numManifests += 1
				Expect(common.IsSingleNodeCluster(&cluster)).To(BeTrue())
			}
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(numManifests))
			Expect(openshiftManifests["99_openshift-cnv_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["99_openshift-cnv_operator_group.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["99_openshift-cnv_subscription.yaml"]).NotTo(HaveLen(0))
			if isSno && cfg.SNOInstallHPP {
				Expect(string(manifest)).To(ContainSubstring("HostPathProvisioner"))
				Expect(openshiftManifests["99_openshift-cnv_hpp_sc.yaml"]).NotTo(HaveLen(0))
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}
		},
			table.Entry("for non-SNO cluster", common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				HighAvailabilityMode: &fullHaMode,
			}}, false, Config{Mode: true, SNOInstallHPP: true}),
			table.Entry("for SNO cluster", common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				HighAvailabilityMode: &noneHaMode,
			}}, true, Config{Mode: true, SNOInstallHPP: true}),
			table.Entry("for SNO cluster and opt out of HPP via env var", common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				HighAvailabilityMode: &noneHaMode,
			}}, true, Config{Mode: true, SNOInstallHPP: false}),
		)

		It("Should create downstream manifests", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			}}
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
