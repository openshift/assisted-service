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
	"k8s.io/utils/ptr"
)

const (
	requirementsEnv = "HW_VALIDATOR_REQUIREMENTS"
)

func vrd(cpuCores, ramMib, diskSizeGb, diskSpeedMs int64) *models.VersionedClusterHostRequirementsDetails {
	return &models.VersionedClusterHostRequirementsDetails{
		CPUCores:                         ptr.To(cpuCores),
		RAMMib:                           ptr.To(ramMib),
		DiskSizeGb:                       ptr.To(diskSizeGb),
		InstallationDiskSpeedThresholdMs: ptr.To(diskSpeedMs),
	}
}

var _ = Describe("Versioned Requirements", func() {
	BeforeEach(func() {
		_ = os.Unsetenv(requirementsEnv)
	})

	AfterEach(func() {
		_ = os.Unsetenv(requirementsEnv)
	})

	When("loaded", func() {
		It("should return error when no env variable is set", func() {
			cfg := ValidatorCfg{}

			err := envconfig.Process("", &cfg)

			Expect(err).ToNot(HaveOccurred())
			_, err = cfg.VersionedRequirements.GetVersionedHostRequirements("default")
			Expect(err).To(HaveOccurred())
		})

		It("should decode a single full version entry", func() {
			jsonData := []map[string]interface{}{
				{
					"version": "4.6.0",
					"master": map[string]interface{}{
						"cpu_cores":                            4,
						"ram_mib":                              16384,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 2,
					},
					"arbiter": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
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
			cfg, err := configureRequirements(jsonData)
			Expect(err).ToNot(HaveOccurred())

			requirements, err := cfg.VersionedRequirements.GetVersionedHostRequirements("4.6.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(*requirements).To(BeEquivalentTo(models.VersionedHostRequirements{
				Version:                "4.6.0",
				MasterRequirements:     vrd(4, conversions.GibToMib(16), 100, 2),
				ArbiterRequirements:    vrd(2, conversions.GibToMib(8), 100, 2),
				WorkerRequirements:     vrd(2, conversions.GibToMib(8), 100, 3),
				SNORequirements:        vrd(8, conversions.GibToMib(32), 100, 4),
				EdgeWorkerRequirements: vrd(2, conversions.GibToMib(8), 100, 3),
			}))
		})

		It("should decode two full version entries", func() {
			jsonData := []map[string]interface{}{
				{
					"version": "4.6.0",
					"master": map[string]interface{}{
						"cpu_cores":                            4,
						"ram_mib":                              16384,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 2,
					},
					"arbiter": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
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
				},
				{
					"version": "4.7.0",
					"master": map[string]interface{}{
						"cpu_cores":                            5,
						"ram_mib":                              17408,
						"disk_size_gb":                         101,
						"installation_disk_speed_threshold_ms": 3,
					},
					"arbiter": map[string]interface{}{
						"cpu_cores":                            3,
						"ram_mib":                              9216,
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
			}
			cfg, err := configureRequirements(jsonData)
			Expect(err).ToNot(HaveOccurred())

			req46, err := cfg.VersionedRequirements.GetVersionedHostRequirements("4.6.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(*req46).To(BeEquivalentTo(models.VersionedHostRequirements{
				Version:                "4.6.0",
				MasterRequirements:     vrd(4, conversions.GibToMib(16), 100, 2),
				ArbiterRequirements:    vrd(2, conversions.GibToMib(8), 100, 2),
				WorkerRequirements:     vrd(2, conversions.GibToMib(8), 100, 1),
				SNORequirements:        vrd(8, conversions.GibToMib(32), 100, 4),
				EdgeWorkerRequirements: vrd(2, conversions.GibToMib(8), 100, 1),
			}))

			req47, err := cfg.VersionedRequirements.GetVersionedHostRequirements("4.7.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(*req47).To(BeEquivalentTo(models.VersionedHostRequirements{
				Version:             "4.7.0",
				MasterRequirements:  vrd(5, conversions.GibToMib(17), 101, 3),
				ArbiterRequirements: vrd(3, conversions.GibToMib(9), 101, 3),
				WorkerRequirements: &models.VersionedClusterHostRequirementsDetails{
					CPUCores:   ptr.To(int64(3)),
					RAMMib:     ptr.To(conversions.GibToMib(9)),
					DiskSizeGb: ptr.To(int64(102)),
				},
				SNORequirements: vrd(7, conversions.GibToMib(31), 103, 4),
				EdgeWorkerRequirements: &models.VersionedClusterHostRequirementsDetails{
					CPUCores:   ptr.To(int64(3)),
					RAMMib:     ptr.To(conversions.GibToMib(9)),
					DiskSizeGb: ptr.To(int64(102)),
				},
			}))
		})

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

		table.DescribeTable("should not be decoded due to duplicate entries", func(jsonData []map[string]interface{}) {
			_, err := configureRequirements(jsonData)
			Expect(err).To(HaveOccurred())
		},
			table.Entry("duplicate version entries",
				[]map[string]interface{}{
					{"version": "4.6.0", "master": map[string]interface{}{"cpu_cores": 1, "ram_mib": 1, "disk_size_gb": 1, "installation_disk_speed_threshold_ms": 0}, "worker": map[string]interface{}{"cpu_cores": 1, "ram_mib": 1, "disk_size_gb": 1, "installation_disk_speed_threshold_ms": 0}, "sno": map[string]interface{}{"cpu_cores": 1, "ram_mib": 1, "disk_size_gb": 1, "installation_disk_speed_threshold_ms": 0}},
					{"version": "4.6.0", "master": map[string]interface{}{"cpu_cores": 2, "ram_mib": 2, "disk_size_gb": 2, "installation_disk_speed_threshold_ms": 0}, "worker": map[string]interface{}{"cpu_cores": 2, "ram_mib": 2, "disk_size_gb": 2, "installation_disk_speed_threshold_ms": 0}, "sno": map[string]interface{}{"cpu_cores": 2, "ram_mib": 2, "disk_size_gb": 2, "installation_disk_speed_threshold_ms": 0}},
				},
			),
			table.Entry("duplicate min_version entries",
				[]map[string]interface{}{
					{"version": "default", "master": map[string]interface{}{"cpu_cores": 4, "ram_mib": 16384, "disk_size_gb": 100, "installation_disk_speed_threshold_ms": 0}, "worker": map[string]interface{}{"cpu_cores": 2, "ram_mib": 8192, "disk_size_gb": 100, "installation_disk_speed_threshold_ms": 0}, "sno": map[string]interface{}{"cpu_cores": 8, "ram_mib": 16384, "disk_size_gb": 100, "installation_disk_speed_threshold_ms": 0}},
					{"version": "4.22", "match_type": "min_version", "sno": map[string]interface{}{"cpu_cores": 4}},
					{"version": "4.22", "match_type": "min_version", "sno": map[string]interface{}{"cpu_cores": 4}},
				},
			),
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
					"arbiter": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
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
			Expect(*requirements).To(BeEquivalentTo(models.VersionedHostRequirements{
				Version:                "4.6.0",
				MasterRequirements:     vrd(4, conversions.GibToMib(16), 100, 2),
				ArbiterRequirements:    vrd(2, conversions.GibToMib(8), 100, 2),
				WorkerRequirements:     vrd(2, conversions.GibToMib(8), 100, 3),
				SNORequirements:        vrd(8, conversions.GibToMib(32), 100, 4),
				EdgeWorkerRequirements: vrd(2, conversions.GibToMib(8), 100, 3),
			}))
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
					"arbiter": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
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
					"arbiter": map[string]interface{}{
						"cpu_cores":                            3,
						"ram_mib":                              9216,
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
			Expect(*requirements).To(BeEquivalentTo(models.VersionedHostRequirements{
				Version:                "default",
				MasterRequirements:     vrd(4, conversions.GibToMib(16), 100, 2),
				ArbiterRequirements:    vrd(2, conversions.GibToMib(8), 100, 2),
				WorkerRequirements:     vrd(2, conversions.GibToMib(8), 100, 3),
				SNORequirements:        vrd(8, conversions.GibToMib(32), 100, 4),
				EdgeWorkerRequirements: vrd(2, conversions.GibToMib(8), 100, 3),
			}))
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
					"arbiter": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
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

			expected := models.VersionedHostRequirements{
				Version:                "4.6.0",
				MasterRequirements:     vrd(4, conversions.GibToMib(16), 100, 2),
				ArbiterRequirements:    vrd(2, conversions.GibToMib(8), 100, 2),
				WorkerRequirements:     vrd(2, conversions.GibToMib(8), 100, 3),
				SNORequirements:        vrd(8, conversions.GibToMib(32), 100, 4),
				EdgeWorkerRequirements: vrd(2, conversions.GibToMib(8), 100, 3),
			}

			requirements, err := cfg.VersionedRequirements.GetVersionedHostRequirements("4.6.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(*requirements).To(BeEquivalentTo(expected))

			*requirements.MasterRequirements.CPUCores = 1
			*requirements.MasterRequirements.RAMMib = 2
			*requirements.WorkerRequirements.CPUCores = 1
			*requirements.WorkerRequirements.RAMMib = 2
			*requirements.SNORequirements.CPUCores = 1
			*requirements.SNORequirements.RAMMib = 2
			requirements.Version = "foo"

			requirements, err = cfg.VersionedRequirements.GetVersionedHostRequirements("4.6.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(*requirements).To(BeEquivalentTo(expected))
		})

		It("should fail when no version entry matches and no default exists", func() {
			jsonSpec := []map[string]interface{}{
				{
					"version": "4.7.0",
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

		It("should fail at decode when min_version entries exist without a default", func() {
			jsonSpec := []map[string]interface{}{
				{
					"version":    "4.22",
					"match_type": "min_version",
					"sno": map[string]interface{}{
						"cpu_cores": 4,
					},
				},
			}

			_, err := configureRequirements(jsonSpec)

			Expect(err).To(HaveOccurred())
		})

		It("should return best min_version match when no exact version is defined", func() {
			jsonSpec := []map[string]interface{}{
				{
					"version": "default",
					"master": map[string]interface{}{
						"cpu_cores":                            4,
						"ram_mib":                              16384,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 2,
					},
					"arbiter": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
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
					"version":    "4.22",
					"match_type": "min_version",
					"sno": map[string]interface{}{
						"cpu_cores": 4,
					},
				},
			}
			cfg, err := configureRequirements(jsonSpec)
			Expect(err).ToNot(HaveOccurred())

			requirements, err := cfg.VersionedRequirements.GetVersionedHostRequirements("4.22.1")
			Expect(err).ToNot(HaveOccurred())
			Expect(*requirements.SNORequirements.CPUCores).To(BeEquivalentTo(4))

			requirements, err = cfg.VersionedRequirements.GetVersionedHostRequirements("4.23.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(*requirements.SNORequirements.CPUCores).To(BeEquivalentTo(4))

			requirements, err = cfg.VersionedRequirements.GetVersionedHostRequirements("4.21.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(*requirements.SNORequirements.CPUCores).To(BeEquivalentTo(8))

			// pre-release versions (e.g. ec, rc) should match as if they were the base release
			requirements, err = cfg.VersionedRequirements.GetVersionedHostRequirements("4.22.0-ec.5")
			Expect(err).ToNot(HaveOccurred())
			Expect(*requirements.SNORequirements.CPUCores).To(BeEquivalentTo(4))
		})

		It("should merge partial min_version fields with default", func() {
			jsonSpec := []map[string]interface{}{
				{
					"version": "default",
					"master": map[string]interface{}{
						"cpu_cores":                            4,
						"ram_mib":                              16384,
						"disk_size_gb":                         100,
						"installation_disk_speed_threshold_ms": 2,
					},
					"arbiter": map[string]interface{}{
						"cpu_cores":                            2,
						"ram_mib":                              8192,
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
					"version":    "4.22",
					"match_type": "min_version",
					"sno": map[string]interface{}{
						"cpu_cores": 4,
					},
				},
			}
			cfg, err := configureRequirements(jsonSpec)
			Expect(err).ToNot(HaveOccurred())

			requirements, err := cfg.VersionedRequirements.GetVersionedHostRequirements("4.22")
			Expect(err).ToNot(HaveOccurred())
			// sno.cpu_cores overridden; other sno fields and all other roles inherited from default
			Expect(*requirements.SNORequirements.CPUCores).To(BeEquivalentTo(4))
			Expect(*requirements.SNORequirements.RAMMib).To(BeEquivalentTo(conversions.GibToMib(32)))
			Expect(*requirements.SNORequirements.DiskSizeGb).To(BeEquivalentTo(100))
			Expect(*requirements.MasterRequirements.CPUCores).To(BeEquivalentTo(4))
			Expect(*requirements.WorkerRequirements.CPUCores).To(BeEquivalentTo(2))
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
