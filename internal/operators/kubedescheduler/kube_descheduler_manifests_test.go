package kubedescheduler

import (
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gopkg.in/yaml.v3"
)

var _ = Describe("Manifest generation", func() {
	var (
		cluster  *common.Cluster
		operator *operator
	)

	BeforeEach(func() {
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				OpenshiftVersion: "4.12.0",
			},
		}
		operator = NewKubeDeschedulerOperator(common.GetTestLog())
	})

	It("Generates the required manifests", func() {
		manifests, _, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(manifests).To(HaveKey("50_kube_descheduler_namespace.yaml"))
		Expect(manifests).To(HaveKey("50_kube_descheduler_subscription.yaml"))
		Expect(manifests).To(HaveKey("50_kube_descheduler_operatorgroup.yaml"))
	})

	It("Generates valid YAML", func() {
		openShiftManifests, customManifest, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		for _, openShiftManifest := range openShiftManifests {
			var object any
			err = yaml.Unmarshal(openShiftManifest, &object)
			Expect(err).ToNot(HaveOccurred())
		}
		var object any
		err = yaml.Unmarshal(customManifest, &object)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("CNV not enabled", func() {
		It("Uses default template when no virtualization operators", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				{Name: "kubedescheduler"},
			}
			_, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())

			// Should contain AffinityAndTaints profile (default)
			Expect(string(customManifest)).To(ContainSubstring("AffinityAndTaints"))
			Expect(string(customManifest)).To(ContainSubstring("mode: Predictive"))
			Expect(string(customManifest)).To(ContainSubstring("deschedulingIntervalSeconds: 3600"))
		})

		It("Uses default template with empty operators list", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{}
			_, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())

			// Should contain AffinityAndTaints profile (default)
			Expect(string(customManifest)).To(ContainSubstring("AffinityAndTaints"))
			Expect(string(customManifest)).To(ContainSubstring("mode: Predictive"))
		})
	})

	Context("CNV (Container Native Virtualization) enabled", func() {
		BeforeEach(func() {
			// Add CNV operator to indicate KubeVirt/Virtualization is enabled
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				{Name: "cnv"},
				{Name: "kubedescheduler"},
			}
		})

		It("Uses KubeVirtRelieveAndMigrate profile for OCP 4.20+ with CNV", func() {
			cluster.OpenshiftVersion = "4.20.0"
			_, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())

			manifestStr := string(customManifest)
			Expect(manifestStr).To(ContainSubstring("KubeVirtRelieveAndMigrate"))
			Expect(manifestStr).To(ContainSubstring("mode: \"Automatic\""))
			Expect(manifestStr).To(ContainSubstring("deschedulingIntervalSeconds: 60"))
			Expect(manifestStr).ToNot(ContainSubstring("profileCustomizations"))
		})

		It("Uses KubeVirtRelieveAndMigrate profile for OCP 4.21.0 with CNV", func() {
			cluster.OpenshiftVersion = "4.21.0"
			_, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())

			manifestStr := string(customManifest)
			Expect(manifestStr).To(ContainSubstring("KubeVirtRelieveAndMigrate"))
			Expect(manifestStr).To(ContainSubstring("mode: \"Automatic\""))
		})

		It("Uses DevKubeVirtRelieveAndMigrate profile for OCP 4.19 with CNV", func() {
			cluster.OpenshiftVersion = "4.19.0"
			_, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())

			manifestStr := string(customManifest)
			Expect(manifestStr).To(ContainSubstring("DevKubeVirtRelieveAndMigrate"))
			Expect(manifestStr).To(ContainSubstring("mode: \"Automatic\""))
			Expect(manifestStr).To(ContainSubstring("deschedulingIntervalSeconds: 60"))
			Expect(manifestStr).To(ContainSubstring("profileCustomizations"))
			Expect(manifestStr).To(ContainSubstring("devEnableSoftTainter: true"))
			Expect(manifestStr).To(ContainSubstring("devDeviationThresholds: AsymmetricLow"))
			Expect(manifestStr).To(ContainSubstring("devActualUtilizationProfile: PrometheusCPUCombined"))
		})

		It("Uses multiple profiles for OCP 4.18 with CNV", func() {
			cluster.OpenshiftVersion = "4.18.0"
			_, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())

			manifestStr := string(customManifest)
			Expect(manifestStr).To(ContainSubstring("LongLifecycle"))
			Expect(manifestStr).To(ContainSubstring("EvictPodsWithPVC"))
			Expect(manifestStr).To(ContainSubstring("EvictPodsWithLocalStorage"))
			Expect(manifestStr).To(ContainSubstring("mode: \"Automatic\""))
			Expect(manifestStr).To(ContainSubstring("deschedulingIntervalSeconds: 60"))
			Expect(manifestStr).ToNot(ContainSubstring("profileCustomizations"))
		})

		It("Uses multiple profiles for OCP 4.17 with CNV", func() {
			cluster.OpenshiftVersion = "4.17.0"
			_, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())

			manifestStr := string(customManifest)
			Expect(manifestStr).To(ContainSubstring("LongLifecycle"))
			Expect(manifestStr).To(ContainSubstring("EvictPodsWithPVC"))
			Expect(manifestStr).To(ContainSubstring("EvictPodsWithLocalStorage"))
		})

		It("Handles invalid version gracefully", func() {
			cluster.OpenshiftVersion = "invalid-version"
			_, _, err := operator.GenerateManifests(cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to select virtualization template"))
		})

		It("Generates valid YAML for all CNV/Virtualization templates", func() {
			versions := []string{"4.17.0", "4.18.0", "4.19.0", "4.20.0", "4.21.0"}

			for _, version := range versions {
				cluster.OpenshiftVersion = version
				_, customManifest, err := operator.GenerateManifests(cluster)
				Expect(err).ToNot(HaveOccurred(), "Failed for version %s", version)

				// Parse YAML to ensure it's valid
				manifests := strings.Split(string(customManifest), "---")
				for _, manifest := range manifests {
					if strings.TrimSpace(manifest) == "" {
						continue
					}
					var object any
					err = yaml.Unmarshal([]byte(manifest), &object)
					Expect(err).ToNot(HaveOccurred(), "Invalid YAML for version %s: %s", version, manifest)
				}
			}
		})
	})
})
