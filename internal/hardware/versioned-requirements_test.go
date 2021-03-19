package hardware

import (
	"os"

	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
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

	table.DescribeTable("should be decoded from JSON", func(json string, expected map[string]VersionedRequirements) {
		_ = os.Setenv(requirementsEnv, json)
		cfg := ValidatorCfg{}

		err := envconfig.Process("", &cfg)

		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.VersionedRequirements).To(BeEquivalentTo(expected))
	},
		table.Entry("empty", "[]", map[string]VersionedRequirements{}),
		table.Entry("One version - full",
			`[{
  			"version": "4.6.0",
  			"master": {
  			  "cpu_cores": 4,
  			  "ram_gib": 16,
  			  "disk_size_gb": 120
  			},
  			"worker": {
  			  "cpu_cores": 2,
  			  "ram_gib": 8,
  			  "disk_size_gb": 120
  			}
		}]`,
			map[string]VersionedRequirements{
				"4.6.0": {
					Version:            "4.6.0",
					MasterRequirements: &Requirements{CPUCores: 4, DiskSizeGb: 120, RAMGib: 16},
					WorkerRequirements: &Requirements{CPUCores: 2, DiskSizeGb: 120, RAMGib: 8},
				}}),
		table.Entry("One version - sparse",
			`[{
  			"version": "4.6.0",
  			"master": {
  			  "ram_gib": 16,
  			  "disk_size_gb": 120
  			},
  			"worker": {
  			  "cpu_cores": 2,
  			  "disk_size_gb": 120
  			}
		}]`,
			map[string]VersionedRequirements{
				"4.6.0": {
					Version:            "4.6.0",
					MasterRequirements: &Requirements{CPUCores: 0, DiskSizeGb: 120, RAMGib: 16},
					WorkerRequirements: &Requirements{CPUCores: 2, DiskSizeGb: 120, RAMGib: 0},
				}}),
		table.Entry("Two versions - full",
			`[{
  			"version": "4.6.0",
  			"master": {
  			  "cpu_cores": 4,
  			  "ram_gib": 16,
  			  "disk_size_gb": 120
  			},
  			"worker": {
  			  "cpu_cores": 2,
  			  "ram_gib": 8,
  			  "disk_size_gb": 120
  			}
		}, {
  			"version": "4.7.0",
  			"master": {
  			  "cpu_cores": 5,
  			  "ram_gib": 17,
  			  "disk_size_gb": 121
  			},
  			"worker": {
  			  "cpu_cores": 3,
  			  "ram_gib": 9,
  			  "disk_size_gb": 122
  			}
		}]`,
			map[string]VersionedRequirements{
				"4.6.0": {
					Version:            "4.6.0",
					MasterRequirements: &Requirements{CPUCores: 4, DiskSizeGb: 120, RAMGib: 16},
					WorkerRequirements: &Requirements{CPUCores: 2, DiskSizeGb: 120, RAMGib: 8},
				},
				"4.7.0": {Version: "4.7.0",
					MasterRequirements: &Requirements{CPUCores: 5, DiskSizeGb: 121, RAMGib: 17},
					WorkerRequirements: &Requirements{CPUCores: 3, DiskSizeGb: 122, RAMGib: 9},
				}}),
		table.Entry("Two versions - one master, one worker",
			`[{
  			"version": "4.6.0",
  			"master": {
  			  "cpu_cores": 4,
  			  "ram_gib": 16,
  			  "disk_size_gb": 120
  			}
		}, {
  			"version": "4.7.0",
  			"worker": {
  			  "cpu_cores": 3,
  			  "ram_gib": 9,
  			  "disk_size_gb": 122
  			}
		}]`,
			map[string]VersionedRequirements{
				"4.6.0": {
					Version:            "4.6.0",
					MasterRequirements: &Requirements{CPUCores: 4, DiskSizeGb: 120, RAMGib: 16},
					WorkerRequirements: nil,
				},
				"4.7.0": {
					Version:            "4.7.0",
					MasterRequirements: nil,
					WorkerRequirements: &Requirements{CPUCores: 3, DiskSizeGb: 122, RAMGib: 9},
				}}),
	)
})
