package hardware

import (
	"encoding/json"
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
		_ = os.Unsetenv(requirementsEnv)
	})

	AfterEach(func() {
		_ = os.Unsetenv(requirementsEnv)
	})
	When("loaded", func() {
		It("should be set to default when no env variable", func() {
			cfg := ValidatorCfg{}

			err := envconfig.Process("", &cfg)

			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.VersionedRequirements).To(BeEmpty())
		})

		table.DescribeTable("should be decoded from JSON", func(jsonData []map[string]interface{}, expected map[string]models.VersionedHostRequirements) {
			cfg, err := configureRequirements(jsonData)

			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.VersionedRequirements).To(BeEquivalentTo(expected))
		},
			table.Entry("empty", []map[string]interface{}{}, map[string]models.VersionedHostRequirements{}),
			table.Entry("One version - full",
				[]map[string]interface{}{
					{
						"version": "4.6.0",
						"master": map[string]interface{}{
							"cpu_cores":                            4,
							"ram_mib":                              16384,
							"disk_size_gb":                         100,
							"installation_disk_speed_threshold_ms": 2,
						},
						"worker": map[string]interface{}{
							"cpu_cores":                            2,
							"ram_mib":                              8192,
							"disk_size_gb":                         100,
							"installation_disk_speed_threshold_ms": 3,
						},
						"sno": map[string]interface{}{
							"cpu_cores":                            8,
							"ram_mib":                              32768,
							"disk_size_gb":                         100,
							"installation_disk_speed_threshold_ms": 4,
						},
						"edge-worker": map[string]interface{}{
							"cpu_cores":                            2,
							"ram_mib":                              8192,
							"disk_size_gb":                         100,
							"installation_disk_speed_threshold_ms": 3,
						},
					},
				},
				map[string]models.VersionedHostRequirements{
					"4.6.0": {
						Version: "4.6.0",
						MasterRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, DiskSizeGb: 100,
							RAMMib: conversions.GibToMib(16), InstallationDiskSpeedThresholdMs: 2},
						WorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 100,
							RAMMib: conversions.GibToMib(8), InstallationDiskSpeedThresholdMs: 3},
						SNORequirements: &models.ClusterHostRequirementsDetails{CPUCores: 8, DiskSizeGb: 100,
							RAMMib: conversions.GibToMib(32), InstallationDiskSpeedThresholdMs: 4},
						EdgeWorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 100,
							RAMMib: conversions.GibToMib(8), InstallationDiskSpeedThresholdMs: 3},
					}}),
			table.Entry("Two versions - full",
				[]map[string]interface{}{
					{
						"version": "4.6.0",
						"master": map[string]interface{}{
							"cpu_cores":                            4,
							"ram_mib":                              16384,
							"disk_size_gb":                         100,
							"installation_disk_speed_threshold_ms": 2,
						},
						"worker": map[string]interface{}{
							"cpu_cores":                            2,
							"ram_mib":                              8192,
							"disk_size_gb":                         100,
							"installation_disk_speed_threshold_ms": 1,
						},
						"sno": map[string]interface{}{
							"cpu_cores":                            8,
							"ram_mib":                              32768,
							"disk_size_gb":                         100,
							"installation_disk_speed_threshold_ms": 4,
						},
						"edge-worker": map[string]interface{}{
							"cpu_cores":                            2,
							"ram_mib":                              8192,
							"disk_size_gb":                         100,
							"installation_disk_speed_threshold_ms": 1,
						},
					}, {
						"version": "4.7.0",
						"master": map[string]interface{}{
							"cpu_cores":                            5,
							"ram_mib":                              17408,
							"disk_size_gb":                         101,
							"installation_disk_speed_threshold_ms": 3,
						},
						"worker": map[string]interface{}{
							"cpu_cores":    3,
							"ram_mib":      9216,
							"disk_size_gb": 102,
						},
						"sno": map[string]interface{}{
							"cpu_cores":                            7,
							"ram_mib":                              31744,
							"disk_size_gb":                         103,
							"installation_disk_speed_threshold_ms": 4,
						},
						"edge-worker": map[string]interface{}{
							"cpu_cores":    3,
							"ram_mib":      9216,
							"disk_size_gb": 102,
						},
					},
				},
				map[string]models.VersionedHostRequirements{
					"4.6.0": {
						Version: "4.6.0",
						MasterRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, DiskSizeGb: 100,
							RAMMib: conversions.GibToMib(16), InstallationDiskSpeedThresholdMs: 2},
						WorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 100,
							RAMMib: conversions.GibToMib(8), InstallationDiskSpeedThresholdMs: 1},
						SNORequirements: &models.ClusterHostRequirementsDetails{CPUCores: 8, DiskSizeGb: 100,
							RAMMib: conversions.GibToMib(32), InstallationDiskSpeedThresholdMs: 4},
						EdgeWorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 100,
							RAMMib: conversions.GibToMib(8), InstallationDiskSpeedThresholdMs: 1},
					},
					"4.7.0": {Version: "4.7.0",
						MasterRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 5, DiskSizeGb: 101,
							RAMMib: conversions.GibToMib(17), InstallationDiskSpeedThresholdMs: 3},
						WorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 3, DiskSizeGb: 102, RAMMib: conversions.GibToMib(9)},
						SNORequirements: &models.ClusterHostRequirementsDetails{CPUCores: 7, DiskSizeGb: 103,
							RAMMib: conversions.GibToMib(31), InstallationDiskSpeedThresholdMs: 4},
						EdgeWorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 3, DiskSizeGb: 102, RAMMib: conversions.GibToMib(9)},
					}}),
		)

		table.DescribeTable("should not be decoded due to missing node requirements", func(role string) {
			jsonData := []map[string]interface{}{
				{
					"version": "4.6.0",
					role: map[string]interface{}{
						"cpu_cores":                            1,
						"ram_mib":                              1,
						"disk_size_gb":                         1,
						"installation_disk_speed_threshold_ms": 0,
					},
				},
			}

			_, err := configureRequirements(jsonData)

			Expect(err).To(HaveOccurred())
		},
			table.Entry("master", "master"),
			table.Entry("worker", "worker"),
			table.Entry("sno", "sno"),
		)

		table.DescribeTable("should not be decoded due to values validation problems", func(role string, cpu, ram, disk, diskSpeed int) {
			validRequirements := map[string]interface{}{
				"cpu_cores":                            1,
				"ram_mib":                              1,
				"disk_size_gb":                         1,
				"installation_disk_speed_threshold_ms": 0,
			}
			jsonData := []map[string]interface{}{
				{
					"version": "4.6.0",
					"master":  validRequirements,
					"worker":  validRequirements,
					"sno":     validRequirements,
				},
			}
			jsonData[0][role] = map[string]interface{}{
				"cpu_cores":                            cpu,
				"ram_mib":                              ram,
				"disk_size_gb":                         disk,
				"installation_disk_speed_threshold_ms": diskSpeed,
			}

			_, err := configureRequirements(jsonData)

			Expect(err).To(HaveOccurred())
		},
			table.Entry("master: zero CPU", "master", 0, 1, 1, 1),
			table.Entry("master: zero RAM", "master", 1, 0, 1, 1),
			table.Entry("master: zero disk", "master", 1, 1, 0, 1),

			table.Entry("master: negative CPU", "master", -1, 1, 1, 1),
			table.Entry("master: negative RAM", "master", 1, -1, 1, 1),
			table.Entry("master: negative disk", "master", 1, 1, -1, 1),
			table.Entry("master: negative disk sped", "master", 1, 1, 1, -1),

			table.Entry("worker: zero CPU", "worker", 0, 1, 1, 1),
			table.Entry("worker: zero RAM", "worker", 1, 0, 1, 1),
			table.Entry("worker: zero disk", "worker", 1, 1, 0, 1),

			table.Entry("worker: negative CPU", "worker", -1, 1, 1, 1),
			table.Entry("worker: negative RAM", "worker", 1, -1, 1, 1),
			table.Entry("worker: negative disk", "worker", 1, 1, -1, 1),
			table.Entry("worker: negative disk speed", "worker", 1, 1, 1, -1),

			table.Entry("sno: zero CPU", "sno", 0, 1, 1, 1),
			table.Entry("sno: zero RAM", "sno", 1, 0, 1, 1),
			table.Entry("sno: zero disk", "sno", 1, 1, 0, 1),

			table.Entry("sno: negative CPU", "sno", -1, 1, 1, 1),
			table.Entry("sno: negative RAM", "sno", 1, -1, 1, 1),
			table.Entry("sno: negative disk", "sno", 1, 1, -1, 1),
			table.Entry("sno: negative disk speed", "sno", 1, 1, 1, -1),
		)
	})

	When("queried", func() {
		It("should be returned when defined", func() {
			jsonSpec := []map[string]interface{}{
				{
					"version": "4.6.0",
					"master": map[string]interface{}{
						"cpu_cores":                            4,
						"ram_mib":                              16384,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 2,
					},
					"worker": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 3,
					},
					"sno": map[string]interface{}{
						"cpu_cores":                            8,
						"ram_mib":                              32768,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 4,
					},
					"edge-worker": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 3,
					},
				},
			}
			cfg, err := configureRequirements(jsonSpec)
			Expect(err).ToNot(HaveOccurred())

			requirements, err := cfg.VersionedRequirements.GetVersionedHostRequirements("4.6.0")

			Expect(err).ToNot(HaveOccurred())
			expected := models.VersionedHostRequirements{
				Version: "4.6.0",
				MasterRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, DiskSizeGb: 100,
					RAMMib: conversions.GibToMib(16), InstallationDiskSpeedThresholdMs: 2},
				WorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 100,
					RAMMib: conversions.GibToMib(8), InstallationDiskSpeedThresholdMs: 3},
				SNORequirements: &models.ClusterHostRequirementsDetails{CPUCores: 8, DiskSizeGb: 100,
					RAMMib: conversions.GibToMib(32), InstallationDiskSpeedThresholdMs: 4},
				EdgeWorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 100,
					RAMMib: conversions.GibToMib(8), InstallationDiskSpeedThresholdMs: 3},
			}
			Expect(*requirements).To(BeEquivalentTo(expected))
		})

		It("should return default when queried version not defined", func() {
			jsonSpec := []map[string]interface{}{
				{
					"version": "default",
					"master": map[string]interface{}{
						"cpu_cores":                            4,
						"ram_mib":                              16384,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 2,
					},
					"worker": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 3,
					},
					"sno": map[string]interface{}{
						"cpu_cores":                            8,
						"ram_mib":                              32768,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 4,
					},
				},
				{
					"version": "4.7.0",
					"master": map[string]interface{}{
						"cpu_cores":                            5,
						"ram_mib":                              17408,
						"disk_size_gb":                         101,
						"installation_disk_speed_threshold_ms": 3,
					},
					"worker": map[string]interface{}{
						"cpu_cores":    3,
						"ram_mib":      9216,
						"disk_size_gb": 102,
					},
					"sno": map[string]interface{}{
						"cpu_cores":                            7,
						"ram_mib":                              31744,
						"disk_size_gb":                         103,
						"installation_disk_speed_threshold_ms": 4,
					},
				},
			}
			cfg, err := configureRequirements(jsonSpec)
			Expect(err).ToNot(HaveOccurred())

			requirements, err := cfg.VersionedRequirements.GetVersionedHostRequirements("4.6.0")

			Expect(err).ToNot(HaveOccurred())
			expected := models.VersionedHostRequirements{
				Version: "default",
				MasterRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, DiskSizeGb: 100,
					RAMMib: conversions.GibToMib(16), InstallationDiskSpeedThresholdMs: 2},
				WorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 100,
					RAMMib: conversions.GibToMib(8), InstallationDiskSpeedThresholdMs: 3},
				SNORequirements: &models.ClusterHostRequirementsDetails{CPUCores: 8, DiskSizeGb: 100,
					RAMMib: conversions.GibToMib(32), InstallationDiskSpeedThresholdMs: 4},
				EdgeWorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 100,
					RAMMib: conversions.GibToMib(8), InstallationDiskSpeedThresholdMs: 3},
			}
			Expect(*requirements).To(BeEquivalentTo(expected))
		})

		It("should not be changed when returned value is modified", func() {
			jsonSpec := []map[string]interface{}{
				{
					"version": "4.6.0",
					"master": map[string]interface{}{
						"cpu_cores":                            4,
						"ram_mib":                              16384,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 2,
					},
					"worker": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 3,
					},
					"sno": map[string]interface{}{
						"cpu_cores":                            8,
						"ram_mib":                              32768,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 4,
					},
					"edge-worker": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 3,
					},
				},
			}
			cfg, err := configureRequirements(jsonSpec)
			Expect(err).ToNot(HaveOccurred())

			requirements, err := cfg.VersionedRequirements.GetVersionedHostRequirements("4.6.0")

			Expect(err).ToNot(HaveOccurred())
			expected := models.VersionedHostRequirements{
				Version: "4.6.0",
				MasterRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, DiskSizeGb: 100,
					RAMMib: conversions.GibToMib(16), InstallationDiskSpeedThresholdMs: 2},
				WorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 100,
					RAMMib: conversions.GibToMib(8), InstallationDiskSpeedThresholdMs: 3},
				SNORequirements: &models.ClusterHostRequirementsDetails{CPUCores: 8, DiskSizeGb: 100,
					RAMMib: conversions.GibToMib(32), InstallationDiskSpeedThresholdMs: 4},
				EdgeWorkerRequirements: &models.ClusterHostRequirementsDetails{CPUCores: 2, DiskSizeGb: 100,
					RAMMib: conversions.GibToMib(8), InstallationDiskSpeedThresholdMs: 3},
			}
			Expect(*requirements).To(BeEquivalentTo(expected))

			requirements.MasterRequirements.CPUCores = 1
			requirements.MasterRequirements.RAMMib = 2
			requirements.WorkerRequirements.CPUCores = 1
			requirements.WorkerRequirements.RAMMib = 2
			requirements.SNORequirements.CPUCores = 1
			requirements.SNORequirements.RAMMib = 2
			requirements.Version = "foo"

			requirements, err = cfg.VersionedRequirements.GetVersionedHostRequirements("4.6.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(*requirements).To(BeEquivalentTo(expected))
		})

		It("should fail when requested and default versions not defined", func() {
			jsonSpec := []map[string]interface{}{
				{
					"version": "4.5.0",
					"master": map[string]interface{}{
						"cpu_cores":                            4,
						"ram_mib":                              16384,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 2,
					},
					"worker": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 3,
					},
					"sno": map[string]interface{}{
						"cpu_cores":                            8,
						"ram_mib":                              32768,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 4,
					},
				},
			}
			cfg, err := configureRequirements(jsonSpec)
			Expect(err).ToNot(HaveOccurred())

			_, err = cfg.VersionedRequirements.GetVersionedHostRequirements("4.6.0")

			Expect(err).To(HaveOccurred())
		})
	})
})

func configureRequirements(jsonSpec []map[string]interface{}) (*ValidatorCfg, error) {
	jsonData, err := json.Marshal(jsonSpec)
	if err != nil {
		return nil, err
	}
	_ = os.Setenv(requirementsEnv, string(jsonData))
	cfg := ValidatorCfg{}
	err = envconfig.Process("", &cfg)
	return &cfg, err
}
