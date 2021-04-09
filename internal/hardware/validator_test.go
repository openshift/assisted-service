package hardware

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/alecthomas/units"
	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
)

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Hardware Validator tests Suite")
}

var _ = Describe("Disk eligibility", func() {
	var (
		hwvalidator   Validator
		testDisk      models.Disk
		bigEnoughSize int64
		tooSmallSize  int64
	)

	BeforeEach(func() {
		var cfg ValidatorCfg
		Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).ShouldNot(HaveOccurred())
		hwvalidator = NewValidator(logrus.New(), cfg, nil)

		bigEnoughSize = conversions.GbToBytes(cfg.MinDiskSizeGb) + 1
		tooSmallSize = conversions.GbToBytes(cfg.MinDiskSizeGb) - 1

		// Start off with an eligible default
		testDisk = models.Disk{
			DriveType: "SSD",
			SizeBytes: bigEnoughSize,
		}
	})

	It("Check if SSD is eligible", func() {
		testDisk.DriveType = "SSD"
		Expect(hwvalidator.DiskIsEligible(&testDisk)).To(BeEmpty())
	})

	It("Check if HDD is eligible", func() {
		testDisk.DriveType = "HDD"
		Expect(hwvalidator.DiskIsEligible(&testDisk)).To(BeEmpty())
	})

	It("Check that ODD is not eligible", func() {
		testDisk.DriveType = "ODD"
		Expect(hwvalidator.DiskIsEligible(&testDisk)).ToNot(BeEmpty())
	})

	It("Check that a big enough size is eligible", func() {
		testDisk.SizeBytes = bigEnoughSize
		Expect(hwvalidator.DiskIsEligible(&testDisk)).To(BeEmpty())
	})

	It("Check that a small size is not eligible", func() {
		testDisk.SizeBytes = tooSmallSize
		Expect(hwvalidator.DiskIsEligible(&testDisk)).ToNot(BeEmpty())
	})
})

var _ = Describe("hardware_validator", func() {
	var (
		hwvalidator   Validator
		host1         *models.Host
		host2         *models.Host
		host3         *models.Host
		inventory     *models.Inventory
		cluster       *common.Cluster
		validDiskSize = int64(128849018880)
		status        = models.HostStatusKnown
	)
	BeforeEach(func() {
		var cfg ValidatorCfg
		Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).ShouldNot(HaveOccurred())
		hwvalidator = NewValidator(logrus.New(), cfg, nil)
		id1 := strfmt.UUID(uuid.New().String())
		id2 := strfmt.UUID(uuid.New().String())
		id3 := strfmt.UUID(uuid.New().String())
		clusterID := strfmt.UUID(uuid.New().String())
		host1 = &models.Host{ID: &id1, ClusterID: clusterID, Status: &status, RequestedHostname: "reqhostname1"}
		host2 = &models.Host{ID: &id2, ClusterID: clusterID, Status: &status, RequestedHostname: "reqhostname2"}
		host3 = &models.Host{ID: &id3, ClusterID: clusterID, Status: &status, RequestedHostname: "reqhostname3"}
		inventory = &models.Inventory{
			CPU:    &models.CPU{Count: 16},
			Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB), UsableBytes: int64(32 * units.GiB)},
			Interfaces: []*models.Interface{
				{
					IPV4Addresses: []string{
						"1.2.3.4/24",
					},
				},
			},
			Disks: []*models.Disk{
				{DriveType: "ODD", Name: "loop0"},
				{DriveType: "HDD", Name: "sdb"},
			},
		}
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                 &clusterID,
			MachineNetworkCidr: "1.2.3.0/24",
		}}
		cluster.Hosts = append(cluster.Hosts, host1)
		cluster.Hosts = append(cluster.Hosts, host2)
		cluster.Hosts = append(cluster.Hosts, host3)
	})

	It("validate_disk_list_return_order", func() {
		nvmename := "nvme01fs"

		eligible := models.DiskInstallationEligibility{
			Eligible: true,
		}

		inventory.Disks = []*models.Disk{
			// Not disk type
			{
				DriveType: "ODD", Name: "aaa",
				InstallationEligibility: models.DiskInstallationEligibility{
					Eligible:           false,
					NotEligibleReasons: []string{"Reason"},
				},
			},
			{DriveType: "SSD", Name: nvmename, SizeBytes: validDiskSize + 1, InstallationEligibility: eligible},
			{DriveType: "SSD", Name: "stam", SizeBytes: validDiskSize, InstallationEligibility: eligible},
			{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize + 2, InstallationEligibility: eligible},
			{DriveType: "HDD", Name: "sda", SizeBytes: validDiskSize + 100, InstallationEligibility: eligible},
			{DriveType: "HDD", Name: "sdh", SizeBytes: validDiskSize + 1, InstallationEligibility: eligible},
		}
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host1.Inventory = string(hw)
		disks, err := hwvalidator.GetHostValidDisks(host1)
		Expect(err).NotTo(HaveOccurred())
		Expect(disks[0].Name).Should(Equal("sdh"))
		Expect(len(disks)).Should(Equal(5))
		Expect(isBlockDeviceNameInlist(disks, nvmename)).Should(BeTrue())
		Expect(disks[3].DriveType).To(Equal("SSD"))
		Expect(disks[4].DriveType).To(Equal("SSD"))
		Expect(disks[4].Name).To(HavePrefix("nvme"))
	})

	It("validate_aws_disk_detected", func() {
		inventory.Disks = []*models.Disk{
			{
				Name:                    "xvda",
				SizeBytes:               128849018880,
				ByPath:                  "",
				DriveType:               "SSD",
				Hctl:                    "",
				Model:                   "",
				Path:                    "/dev/xvda",
				Serial:                  "",
				Vendor:                  "",
				Wwn:                     "",
				InstallationEligibility: models.DiskInstallationEligibility{Eligible: true},
			},
		}
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host1.Inventory = string(hw)
		disks, err := hwvalidator.GetHostValidDisks(host1)
		Expect(err).NotTo(HaveOccurred())
		Expect(disks[0].Name).Should(Equal("xvda"))
		Expect(len(disks)).Should(Equal(1))
	})
})

var _ = Describe("Cluster host requirements", func() {

	var (
		cfg         ValidatorCfg
		hwvalidator Validator
		cluster     *common.Cluster
		host        *models.Host

		ctrl          *gomock.Controller
		operatorsMock *operators.MockAPI

		operatorRequirements []*models.OperatorHostRequirements
		details1, details2   models.ClusterHostRequirementsDetails
	)

	var versionRequirementsSource = []map[string]interface{}{
		{
			"version": "4.6",
			"master": map[string]interface{}{
				"cpu_cores":    4,
				"ram_mib":      16384,
				"disk_size_gb": 120,
			},
			"worker": map[string]interface{}{
				"cpu_cores":    2,
				"ram_mib":      8192,
				"disk_size_gb": 120,
			},
		},
		{
			"version": "4.7",
			"master": map[string]interface{}{
				"cpu_cores":                            5,
				"ram_mib":                              17408,
				"disk_size_gb":                         121,
				"installation_disk_speed_threshold_ms": 1,
			},
			"worker": map[string]interface{}{
				"cpu_cores":                            3,
				"ram_mib":                              9216,
				"disk_size_gb":                         122,
				"installation_disk_speed_threshold_ms": 2,
			},
		},
	}

	const (
		prefixedRequirementsEnv   = "MYAPP_" + requirementsEnv
		openShiftVersionNotInJSON = "4.5"
	)

	BeforeEach(func() {
		operatorName1 := "op-one"
		operatorName2 := "op-two"

		clusterID := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			OpenshiftVersion: openShiftVersionNotInJSON,
			MonitoredOperators: []*models.MonitoredOperator{
				{Name: operatorName1, ClusterID: clusterID},
				{Name: operatorName2, ClusterID: clusterID},
			},
		}}
		versionRequirements, err := json.Marshal(versionRequirementsSource)
		Expect(err).ToNot(HaveOccurred())
		_ = os.Setenv(prefixedRequirementsEnv, string(versionRequirements))
		Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).ShouldNot(HaveOccurred())
		Expect(cfg.VersionedRequirements).ToNot(HaveKey(openShiftVersionNotInJSON))
		details1 = models.ClusterHostRequirementsDetails{
			InstallationDiskSpeedThresholdMs: 10,
			RAMMib:                           1024,
			CPUCores:                         4,
			DiskSizeGb:                       10,
		}
		details2 = models.ClusterHostRequirementsDetails{
			InstallationDiskSpeedThresholdMs: 5,
			RAMMib:                           256,
			CPUCores:                         2,
			DiskSizeGb:                       5,
		}

		operatorRequirements = []*models.OperatorHostRequirements{
			{OperatorName: operatorName1, Requirements: &details1},
			{OperatorName: operatorName2, Requirements: &details2},
		}

		ctrl = gomock.NewController(GinkgoT())
		operatorsMock = operators.NewMockAPI(ctrl)

		hwvalidator = NewValidator(logrus.New(), cfg, operatorsMock)
	})

	AfterEach(func() {
		_ = os.Unsetenv(prefixedRequirementsEnv)
		ctrl.Finish()
	})

	It("should contain correct default requirements for master host", func() {
		role := models.HostRoleMaster
		id1 := strfmt.UUID(uuid.New().String())
		host = &models.Host{ID: &id1, ClusterID: *cluster.ID, Role: role}

		operatorsMock.EXPECT().GetRequirementsBreakdownForHostInCluster(gomock.Any(), gomock.Eq(cluster), gomock.Eq(host)).Return(operatorRequirements, nil)

		result, err := hwvalidator.GetClusterHostRequirements(context.TODO(), cluster, host)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())

		Expect(result.Ocp.DiskSizeGb).To(BeEquivalentTo(cfg.MinDiskSizeGb))
		Expect(result.Ocp.CPUCores).To(BeEquivalentTo(cfg.MinCPUCoresMaster))
		Expect(result.Ocp.RAMMib).To(BeEquivalentTo(cfg.MinRamGibMaster * int64(units.KiB)))
		Expect(result.Ocp.InstallationDiskSpeedThresholdMs).To(Equal(cfg.InstallationDiskSpeedThresholdMs))

		Expect(result.Operators).To(ConsistOf(operatorRequirements))

		Expect(result.Total.DiskSizeGb).To(Equal(cfg.MinDiskSizeGb + details1.DiskSizeGb + details2.DiskSizeGb))
		Expect(result.Total.CPUCores).To(Equal(cfg.MinCPUCoresMaster + details1.CPUCores + details2.CPUCores))
		Expect(result.Total.RAMMib).To(Equal(cfg.MinRamGibMaster*int64(units.KiB) + details1.RAMMib + details2.RAMMib))
		Expect(result.Total.InstallationDiskSpeedThresholdMs).To(Equal(details2.InstallationDiskSpeedThresholdMs))
	})

	It("should contain correct default requirements for worker host", func() {
		role := models.HostRoleWorker
		id1 := strfmt.UUID(uuid.New().String())
		host = &models.Host{ID: &id1, ClusterID: *cluster.ID, Role: role}

		operatorsMock.EXPECT().GetRequirementsBreakdownForHostInCluster(gomock.Any(), gomock.Eq(cluster), gomock.Eq(host)).Return(operatorRequirements, nil)

		result, err := hwvalidator.GetClusterHostRequirements(context.TODO(), cluster, host)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())

		Expect(result.Ocp.DiskSizeGb).To(BeEquivalentTo(cfg.MinDiskSizeGb))
		Expect(result.Ocp.CPUCores).To(BeEquivalentTo(cfg.MinCPUCoresWorker))
		Expect(result.Ocp.RAMMib).To(BeEquivalentTo(cfg.MinRamGibWorker * int64(units.KiB)))
		Expect(result.Ocp.InstallationDiskSpeedThresholdMs).To(Equal(cfg.InstallationDiskSpeedThresholdMs))

		Expect(result.Operators).To(ConsistOf(operatorRequirements))

		Expect(result.Total.DiskSizeGb).To(Equal(cfg.MinDiskSizeGb + details1.DiskSizeGb + details2.DiskSizeGb))
		Expect(result.Total.CPUCores).To(Equal(cfg.MinCPUCoresWorker + details1.CPUCores + details2.CPUCores))
		Expect(result.Total.RAMMib).To(Equal(cfg.MinRamGibWorker*int64(units.KiB) + details1.RAMMib + details2.RAMMib))
		Expect(result.Total.InstallationDiskSpeedThresholdMs).To(Equal(details2.InstallationDiskSpeedThresholdMs))
	})

	It("should fail providing on operator API error", func() {
		role := models.HostRoleWorker
		id1 := strfmt.UUID(uuid.New().String())
		host = &models.Host{ID: &id1, ClusterID: *cluster.ID, Role: role}

		failure := errors.New("boom")
		operatorsMock.EXPECT().GetRequirementsBreakdownForHostInCluster(gomock.Any(), gomock.Eq(cluster), gomock.Eq(host)).Return(nil, failure)

		_, err := hwvalidator.GetClusterHostRequirements(context.TODO(), cluster, host)

		Expect(err).To(HaveOccurred())
		Expect(err).To(Equal(failure))
	})

	table.DescribeTable("should contain correct requirements for host role and dedicated OCP version requirements",
		func(role models.HostRole, expectedOcpRequirements models.ClusterHostRequirementsDetails) {

			id1 := strfmt.UUID(uuid.New().String())
			host = &models.Host{ID: &id1, ClusterID: *cluster.ID, Role: role}
			cluster.OpenshiftVersion = "4.7"

			operatorsMock.EXPECT().GetRequirementsBreakdownForHostInCluster(gomock.Any(), gomock.Eq(cluster), gomock.Eq(host)).Return(operatorRequirements, nil)

			result, err := hwvalidator.GetClusterHostRequirements(context.TODO(), cluster, host)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			Expect(*result.Ocp).To(BeEquivalentTo(expectedOcpRequirements))

			Expect(result.Operators).To(ConsistOf(operatorRequirements))

			Expect(result.Total.DiskSizeGb).To(Equal(expectedOcpRequirements.DiskSizeGb + details1.DiskSizeGb + details2.DiskSizeGb))
			Expect(result.Total.CPUCores).To(Equal(expectedOcpRequirements.CPUCores + details1.CPUCores + details2.CPUCores))
			Expect(result.Total.RAMMib).To(Equal(expectedOcpRequirements.RAMMib + details1.RAMMib + details2.RAMMib))
			Expect(result.Total.InstallationDiskSpeedThresholdMs).To(Equal(expectedOcpRequirements.InstallationDiskSpeedThresholdMs))
		},
		table.Entry("Worker", models.HostRoleWorker, models.ClusterHostRequirementsDetails{
			CPUCores:                         3,
			DiskSizeGb:                       122,
			RAMMib:                           9 * int64(units.KiB),
			InstallationDiskSpeedThresholdMs: 2,
		}),
		table.Entry("Master", models.HostRoleMaster, models.ClusterHostRequirementsDetails{
			CPUCores:                         5,
			DiskSizeGb:                       121,
			RAMMib:                           17 * int64(units.KiB),
			InstallationDiskSpeedThresholdMs: 1,
		}),
	)

})

func isBlockDeviceNameInlist(disks []*models.Disk, name string) bool {
	for _, disk := range disks {
		// Valid disk: type=disk, not removable, not readonly and size bigger than minimum required
		if disk.Name == name {
			return true
		}
	}
	return false
}
