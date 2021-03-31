package hardware

import (
	"os"

	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

const (
	requirementsEnv = "HW_VALIDATOR_REQUIREMENTS"
)

var _ = Describe("Versioned Requirements", func() {
	BeforeEach(func() {
		err := os.Unsetenv(requirementsEnv)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should be set to default when no env variable", func() {
		cfg := ValidatorCfg{}

		err := envconfig.Process("", &cfg)

		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.VersionedRequirements).To(BeEmpty())
	})

	table.DescribeTable("should be decoded from JSON", func(json string, expected map[string]models.VersionedHostRequirements) {
		_ = os.Setenv(requirementsEnv, json)
		cfg := ValidatorCfg{}

		err := envconfig.Process("", &cfg)

		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.VersionedRequirements).To(BeEquivalentTo(expected))
	},
		table.Entry("empty", "[]", map[string]models.VersionedHostRequirements{}),
		table.Entry("One version - full",
			`[{
  			"version": "4.6.0",
  			"master": {
  			  "cpu_cores": 4,
  			  "ram_mib": 16384,
  			  "disk_size_gb": 120,
 			  "installation_disk_speed_threshold_ms": 2
  			},
  			"worker": {
  			  "cpu_cores": 2,
  			  "ram_mib": 8192,
  			  "disk_size_gb": 120,
		  	  "installation_disk_speed_threshold_ms": 3
  			}
		}]`,
			map[string]models.VersionedHostRequirements{
				"4.6.0": {
					Version: "4.6.0",
					MasterRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, DiskSizeGb: 120,
						RAMMib: conversions.GibToMib(16), InstallationDiskSpeedThresholdMs: 2},
					WorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 120,
						RAMMib: conversions.GibToMib(8), InstallationDiskSpeedThresholdMs: 3},
				}}),
		table.Entry("One version - sparse",
			`[{
  			"version": "4.6.0",
  			"master": {
  			  "ram_mib": 16384,
  			  "disk_size_gb": 120,
			  "installation_disk_speed_threshold_ms": 3
  			},
  			"worker": {
  			  "cpu_cores": 2,
  			  "disk_size_gb": 120,
			  "installation_disk_speed_threshold_ms": 2
  			}
		}]`,
			map[string]models.VersionedHostRequirements{
				"4.6.0": {
					Version: "4.6.0",
					MasterRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 0, DiskSizeGb: 120,
						RAMMib: conversions.GibToMib(16), InstallationDiskSpeedThresholdMs: 3},
					WorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 120,
						RAMMib: 0, InstallationDiskSpeedThresholdMs: 2},
				}}),
		table.Entry("Two versions - full",
			`[{
  			"version": "4.6.0",
  			"master": {
  			  "cpu_cores": 4,
  			  "ram_mib": 16384,
  			  "disk_size_gb": 120,
			  "installation_disk_speed_threshold_ms": 2
  			},
  			"worker": {
  			  "cpu_cores": 2,
  			  "ram_mib": 8192,
  			  "disk_size_gb": 120,
			  "installation_disk_speed_threshold_ms": 1
  			}
		}, {
  			"version": "4.7.0",
  			"master": {
  			  "cpu_cores": 5,
  			  "ram_mib": 17408,
  			  "disk_size_gb": 121,
			  "installation_disk_speed_threshold_ms": 3
  			},
  			"worker": {
  			  "cpu_cores": 3,
  			  "ram_mib": 9216,
  			  "disk_size_gb": 122
  			}
		}]`,
			map[string]models.VersionedHostRequirements{
				"4.6.0": {
					Version: "4.6.0",
					MasterRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, DiskSizeGb: 120,
						RAMMib: conversions.GibToMib(16), InstallationDiskSpeedThresholdMs: 2},
					WorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 120,
						RAMMib: conversions.GibToMib(8), InstallationDiskSpeedThresholdMs: 1},
				},
				"4.7.0": {Version: "4.7.0",
					MasterRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 5, DiskSizeGb: 121,
						RAMMib: conversions.GibToMib(17), InstallationDiskSpeedThresholdMs: 3},
					WorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 3, DiskSizeGb: 122, RAMMib: conversions.GibToMib(9)},
				}}),
		table.Entry("Two versions - one master, one worker",
			`[{
  			"version": "4.6.0",
  			"master": {
  			  "cpu_cores": 4,
  			  "ram_mib": 16384,
  			  "disk_size_gb": 120, 
			  "installation_disk_speed_threshold_ms": 2
  			}
		}, {
  			"version": "4.7.0",
  			"worker": {
  			  "cpu_cores": 3,
  			  "ram_mib": 9216,
  			  "disk_size_gb": 122,
			  "installation_disk_speed_threshold_ms": 3
  			}
		}]`,
			map[string]models.VersionedHostRequirements{
				"4.6.0": {
					Version: "4.6.0",
					MasterRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, DiskSizeGb: 120,
						RAMMib: conversions.GibToMib(16), InstallationDiskSpeedThresholdMs: 2},
					WorkerRequirements: nil,
				},
				"4.7.0": {
					Version:            "4.7.0",
					MasterRequirements: nil,
					WorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 3, DiskSizeGb: 122,
						RAMMib: conversions.GibToMib(9), InstallationDiskSpeedThresholdMs: 3},
				}}),
	)
})
