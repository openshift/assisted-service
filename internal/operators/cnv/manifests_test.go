package cnv

import (
	"github.com/hashicorp/go-version"
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
	operator := NewCNVOperator(common.GetTestLog(), Config{Mode: true, SNOInstallHPP: true})

	Context("CNV Manifest", func() {
		table.DescribeTable("Should create manifestes", func(cluster common.Cluster, isSno bool, cfg Config) {
			cnvOperator := NewCNVOperator(common.GetTestLog(), cfg)
			openshiftManifests, manifest, err := cnvOperator.GenerateManifests(&cluster)
			numManifests := 3
			if isSno && cfg.SNOInstallHPP {
				var versionerr error
				var ocpVersion, minimalVersionForHppSno *version.Version
				ocpVersion, versionerr = version.NewVersion(cluster.OpenshiftVersion)
				Expect(versionerr).ShouldNot(HaveOccurred())
				minimalVersionForHppSno, versionerr = version.NewVersion(minimalOpenShiftVersionForHPPSNO)
				Expect(versionerr).ShouldNot(HaveOccurred())
				if !ocpVersion.LessThan(minimalVersionForHppSno) {
					// Add Hostpathprovisioner StorageClass to expectation
					numManifests += 1
					Expect(common.IsSingleNodeCluster(&cluster)).To(BeTrue())
					Expect(string(manifest)).To(ContainSubstring("HostPathProvisioner"))
					Expect(openshiftManifests["50_openshift-cnv_hpp_sc.yaml"]).NotTo(HaveLen(0))
				}
			}
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(numManifests))
			Expect(openshiftManifests["50_openshift-cnv_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-cnv_operator_group.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-cnv_subscription.yaml"]).NotTo(HaveLen(0))

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}
		},
			table.Entry("for non-SNO cluster", common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:     "4.10",
				HighAvailabilityMode: &fullHaMode,
			}}, false, Config{Mode: true, SNOInstallHPP: true}),
			table.Entry("for SNO cluster", common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:     "4.10",
				HighAvailabilityMode: &noneHaMode,
			}}, true, Config{Mode: true, SNOInstallHPP: true}),
			table.Entry("for SNO cluster with openshift version (and thus CNV) lower than 4.10", common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:     "4.9",
				HighAvailabilityMode: &noneHaMode,
			}}, true, Config{Mode: true, SNOInstallHPP: true}),
			table.Entry("for SNO cluster and opt out of HPP via env var", common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:     "4.10",
				HighAvailabilityMode: &noneHaMode,
			}}, true, Config{Mode: true, SNOInstallHPP: false}),
		)

		It("Should create downstream manifests", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			}}
			openshiftManifests, _, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(meta(openshiftManifests["50_openshift-cnv_ns.yaml"], "name")).To(Equal("openshift-cnv"))
			Expect(meta(openshiftManifests["50_openshift-cnv_subscription.yaml"], "namespace")).To(Equal("openshift-cnv"))
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
