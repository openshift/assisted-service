package hardware

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"

	"github.com/alecthomas/units"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
	"k8s.io/utils/pointer"
)

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Hardware Validator tests Suite")
}

const (
	defaultMasterCores              = 1
	defaultMasterRam                = 1024
	defaultMasterDiskSize           = 10
	defaultMasterDiskSpeedThreshold = 4
	defaultWorkerCores              = 2
	defaultWorkerRam                = 2048
	defaultWorkerDiskSize           = 20
	defaultWorkerDiskSpeedThreshold = 2
	defaultSnoCores                 = 8
	defaultSnoRam                   = 32768
)

var _ = Describe("Disk eligibility", func() {
	const (
		minDiskSizeGb = 120
	)
	var versionRequirements = VersionedRequirementsDecoder{
		"default": {
			Version: "default",
			MasterRequirements: &models.ClusterHostRequirementsDetails{
				CPUCores:                         defaultMasterCores,
				RAMMib:                           defaultMasterRam,
				DiskSizeGb:                       minDiskSizeGb,
				InstallationDiskSpeedThresholdMs: defaultMasterDiskSpeedThreshold,
			},
			WorkerRequirements: &models.ClusterHostRequirementsDetails{
				CPUCores:                         defaultWorkerCores,
				RAMMib:                           defaultWorkerRam,
				DiskSizeGb:                       minDiskSizeGb,
				InstallationDiskSpeedThresholdMs: defaultWorkerDiskSpeedThreshold,
			},
			SNORequirements: &models.ClusterHostRequirementsDetails{
				CPUCores:                         defaultSnoCores,
				RAMMib:                           defaultSnoRam,
				DiskSizeGb:                       minDiskSizeGb,
				InstallationDiskSpeedThresholdMs: defaultMasterDiskSpeedThreshold,
			},
		},
	}

	var (
		hwvalidator   Validator
		testDisk      models.Disk
		bigEnoughSize int64
		tooSmallSize  int64
		ctx           context.Context
		ctrl          *gomock.Controller
		operatorsMock *operators.MockAPI
		cluster       common.Cluster
		host          models.Host
	)

	BeforeEach(func() {

		clusterID := strfmt.UUID(uuid.New().String())
		cluster = hostutil.GenerateTestCluster(clusterID, "10.0.0.1/24")
		hostID := strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(hostID, clusterID, models.HostStatusDiscovering)

		cfg := ValidatorCfg{VersionedRequirements: versionRequirements}

		ctrl = gomock.NewController(GinkgoT())
		operatorsMock = operators.NewMockAPI(ctrl)
		operatorsMock.EXPECT().GetRequirementsBreakdownForHostInCluster(gomock.Any(), gomock.Any(), gomock.Any()).Return([]*models.OperatorHostRequirements{}, nil)

		hwvalidator = NewValidator(logrus.New(), cfg, operatorsMock)

		bigEnoughSize = conversions.GbToBytes(minDiskSizeGb) + 1
		tooSmallSize = conversions.GbToBytes(minDiskSizeGb) - 1

		// Start off with an eligible default
		testDisk = models.Disk{
			DriveType: "SSD",
			SizeBytes: bigEnoughSize,
		}
		ctx = context.TODO()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("Check if SSD is eligible", func() {
		testDisk.DriveType = "SSD"

		eligible, err := hwvalidator.DiskIsEligible(ctx, &testDisk, &cluster, &host)

		Expect(err).ToNot(HaveOccurred())
		Expect(eligible).To(BeEmpty())
	})

	It("Check if HDD is eligible", func() {
		testDisk.DriveType = "HDD"

		eligible, err := hwvalidator.DiskIsEligible(ctx, &testDisk, &cluster, &host)

		Expect(err).ToNot(HaveOccurred())
		Expect(eligible).To(BeEmpty())
	})

	It("Check that ODD is not eligible", func() {
		testDisk.DriveType = "ODD"

		eligible, err := hwvalidator.DiskIsEligible(ctx, &testDisk, &cluster, &host)

		Expect(err).ToNot(HaveOccurred())
		Expect(eligible).ToNot(BeEmpty())
	})

	It("Check that a big enough size is eligible", func() {
		testDisk.SizeBytes = bigEnoughSize

		eligible, err := hwvalidator.DiskIsEligible(ctx, &testDisk, &cluster, &host)

		Expect(err).ToNot(HaveOccurred())
		Expect(eligible).To(BeEmpty())
	})

	It("Check that a small size is not eligible", func() {
		testDisk.SizeBytes = tooSmallSize

		eligible, err := hwvalidator.DiskIsEligible(ctx, &testDisk, &cluster, &host)

		Expect(err).ToNot(HaveOccurred())
		Expect(eligible).ToNot(BeEmpty())
	})

	It("Check that existing non-eligibility reasons are preserved", func() {
		existingReasons := []string{"Reason 1", "Reason 2"}
		testDisk.InstallationEligibility.NotEligibleReasons = existingReasons

		eligible, err := hwvalidator.DiskIsEligible(ctx, &testDisk, &cluster, &host)

		Expect(err).ToNot(HaveOccurred())
		Expect(eligible).To(ConsistOf(existingReasons))
	})

	It("Check that a small size reason is added to existing reasons", func() {
		existingReasons := []string{"Reason 1", "Reason 2"}
		testDisk.InstallationEligibility.NotEligibleReasons = existingReasons

		testDisk.SizeBytes = tooSmallSize

		eligible, err := hwvalidator.DiskIsEligible(ctx, &testDisk, &cluster, &host)

		Expect(err).ToNot(HaveOccurred())
		Expect(eligible).To(ContainElements(existingReasons))
		Expect(eligible).To(HaveLen(len(existingReasons) + 1))
	})

	It("Check that a old service reasons have been purged", func() {
		operatorsMock.EXPECT().GetRequirementsBreakdownForHostInCluster(gomock.Any(), gomock.Any(), gomock.Any()).Return([]*models.OperatorHostRequirements{}, nil)
		existingReasons := []string{"Reason 1", "Reason 2"}
		testDisk.InstallationEligibility.NotEligibleReasons = existingReasons

		testDisk.SizeBytes = tooSmallSize

		eligible, err := hwvalidator.DiskIsEligible(ctx, &testDisk, &cluster, &host)
		Expect(err).ToNot(HaveOccurred())
		Expect(eligible).To(ContainElements(existingReasons))
		Expect(eligible).To(HaveLen(len(existingReasons) + 1))

		testDisk.InstallationEligibility.NotEligibleReasons = existingReasons
		eligible, err = hwvalidator.DiskIsEligible(ctx, &testDisk, &cluster, &host)
		Expect(err).ToNot(HaveOccurred())
		Expect(eligible).To(ContainElements(existingReasons))
		Expect(eligible).To(HaveLen(len(existingReasons) + 1))
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
			"version": "default",
			"master": map[string]interface{}{
				"cpu_cores":                            defaultMasterCores,
				"ram_mib":                              defaultMasterRam,
				"disk_size_gb":                         defaultMasterDiskSize,
				"installation_disk_speed_threshold_ms": defaultMasterDiskSpeedThreshold,
			},
			"worker": map[string]interface{}{
				"cpu_cores":                            defaultWorkerCores,
				"ram_mib":                              defaultWorkerRam,
				"disk_size_gb":                         defaultWorkerDiskSize,
				"installation_disk_speed_threshold_ms": defaultWorkerDiskSpeedThreshold,
			},
			"sno": map[string]interface{}{
				"cpu_cores":                            defaultSnoCores,
				"ram_mib":                              defaultSnoRam,
				"disk_size_gb":                         defaultMasterDiskSize,
				"installation_disk_speed_threshold_ms": defaultMasterDiskSpeedThreshold,
			},
		},
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
			"sno": map[string]interface{}{
				"cpu_cores":    8,
				"ram_mib":      32768,
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
				"network_latency_threshold_ms":         100,
				"packet_loss_percentage":               0,
			},
			"worker": map[string]interface{}{
				"cpu_cores":                            3,
				"ram_mib":                              9216,
				"disk_size_gb":                         122,
				"installation_disk_speed_threshold_ms": 2,
				"network_latency_threshold_ms":         1000,
				"packet_loss_percentage":               10,
			},
			"sno": map[string]interface{}{
				"cpu_cores":                            7,
				"ram_mib":                              31744,
				"disk_size_gb":                         124,
				"installation_disk_speed_threshold_ms": 3,
				"network_latency_threshold_ms":         1100,
				"packet_loss_percentage":               11,
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
			NetworkLatencyThresholdMs:        pointer.Float64Ptr(100),
			PacketLossPercentage:             pointer.Float64Ptr(0),
		}
		details2 = models.ClusterHostRequirementsDetails{
			InstallationDiskSpeedThresholdMs: 5,
			RAMMib:                           256,
			CPUCores:                         2,
			DiskSizeGb:                       5,
			NetworkLatencyThresholdMs:        pointer.Float64Ptr(1000),
			PacketLossPercentage:             pointer.Float64Ptr(10),
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

		Expect(result.Ocp.DiskSizeGb).To(BeEquivalentTo(defaultMasterDiskSize))
		Expect(result.Ocp.CPUCores).To(BeEquivalentTo(defaultMasterCores))
		Expect(result.Ocp.RAMMib).To(BeEquivalentTo(defaultMasterRam))
		Expect(result.Ocp.InstallationDiskSpeedThresholdMs).To(BeEquivalentTo(defaultMasterDiskSpeedThreshold))

		Expect(result.Operators).To(ConsistOf(operatorRequirements))

		Expect(result.Total.DiskSizeGb).To(BeEquivalentTo(defaultMasterDiskSize + details1.DiskSizeGb + details2.DiskSizeGb))
		Expect(result.Total.CPUCores).To(BeEquivalentTo(defaultMasterCores + details1.CPUCores + details2.CPUCores))
		Expect(result.Total.RAMMib).To(BeEquivalentTo(defaultMasterRam + details1.RAMMib + details2.RAMMib))
		Expect(result.Total.InstallationDiskSpeedThresholdMs).To(BeEquivalentTo(defaultMasterDiskSpeedThreshold))
		Expect(result.Total.NetworkLatencyThresholdMs).To(Equal(details1.NetworkLatencyThresholdMs))
		Expect(result.Total.PacketLossPercentage).To(Equal(details1.PacketLossPercentage))
	})

	It("should contain correct default requirements for sno master host", func() {
		role := models.HostRoleMaster
		id1 := strfmt.UUID(uuid.New().String())
		host = &models.Host{ID: &id1, ClusterID: *cluster.ID, Role: role}
		cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)

		operatorsMock.EXPECT().GetRequirementsBreakdownForHostInCluster(gomock.Any(), gomock.Eq(cluster), gomock.Eq(host)).Return(operatorRequirements, nil)

		result, err := hwvalidator.GetClusterHostRequirements(context.TODO(), cluster, host)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())

		Expect(result.Ocp.DiskSizeGb).To(BeEquivalentTo(defaultMasterDiskSize))
		Expect(result.Ocp.CPUCores).To(BeEquivalentTo(defaultSnoCores))
		Expect(result.Ocp.RAMMib).To(BeEquivalentTo(defaultSnoRam))
		Expect(result.Ocp.InstallationDiskSpeedThresholdMs).To(BeEquivalentTo(defaultMasterDiskSpeedThreshold))

		Expect(result.Operators).To(ConsistOf(operatorRequirements))

		Expect(result.Total.DiskSizeGb).To(BeEquivalentTo(defaultMasterDiskSize + details1.DiskSizeGb + details2.DiskSizeGb))
		Expect(result.Total.CPUCores).To(BeEquivalentTo(defaultSnoCores + details1.CPUCores + details2.CPUCores))
		Expect(result.Total.RAMMib).To(BeEquivalentTo(defaultSnoRam + details1.RAMMib + details2.RAMMib))
		Expect(result.Total.InstallationDiskSpeedThresholdMs).To(BeEquivalentTo(defaultMasterDiskSpeedThreshold))
	})

	It("should contain correct default requirements for worker host", func() {
		role := models.HostRoleWorker
		id1 := strfmt.UUID(uuid.New().String())
		host = &models.Host{ID: &id1, ClusterID: *cluster.ID, Role: role}

		operatorsMock.EXPECT().GetRequirementsBreakdownForHostInCluster(gomock.Any(), gomock.Eq(cluster), gomock.Eq(host)).Return(operatorRequirements, nil)

		result, err := hwvalidator.GetClusterHostRequirements(context.TODO(), cluster, host)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())

		Expect(result.Ocp.DiskSizeGb).To(BeEquivalentTo(defaultWorkerDiskSize))
		Expect(result.Ocp.CPUCores).To(BeEquivalentTo(defaultWorkerCores))
		Expect(result.Ocp.RAMMib).To(BeEquivalentTo(defaultWorkerRam))
		Expect(result.Ocp.InstallationDiskSpeedThresholdMs).To(BeEquivalentTo(defaultWorkerDiskSpeedThreshold))

		Expect(result.Operators).To(ConsistOf(operatorRequirements))

		Expect(result.Total.DiskSizeGb).To(BeEquivalentTo(defaultWorkerDiskSize + details1.DiskSizeGb + details2.DiskSizeGb))
		Expect(result.Total.CPUCores).To(BeEquivalentTo(defaultWorkerCores + details1.CPUCores + details2.CPUCores))
		Expect(result.Total.RAMMib).To(BeEquivalentTo(defaultWorkerRam + details1.RAMMib + details2.RAMMib))
		Expect(result.Total.InstallationDiskSpeedThresholdMs).To(BeEquivalentTo(defaultWorkerDiskSpeedThreshold))
		Expect(result.Total.NetworkLatencyThresholdMs).To(Equal(details1.NetworkLatencyThresholdMs))
		Expect(result.Total.PacketLossPercentage).To(Equal(details1.PacketLossPercentage))
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

			Expect(result.Total.DiskSizeGb).To(BeEquivalentTo(expectedOcpRequirements.DiskSizeGb + details1.DiskSizeGb + details2.DiskSizeGb))
			Expect(result.Total.CPUCores).To(BeEquivalentTo(expectedOcpRequirements.CPUCores + details1.CPUCores + details2.CPUCores))
			Expect(result.Total.RAMMib).To(BeEquivalentTo(expectedOcpRequirements.RAMMib + details1.RAMMib + details2.RAMMib))
			Expect(result.Total.InstallationDiskSpeedThresholdMs).To(BeEquivalentTo(expectedOcpRequirements.InstallationDiskSpeedThresholdMs))
			Expect(result.Total.NetworkLatencyThresholdMs).To(Equal(pointer.Float64Ptr(math.Min(*expectedOcpRequirements.NetworkLatencyThresholdMs, *details1.NetworkLatencyThresholdMs))))
			Expect(result.Total.PacketLossPercentage).To(Equal(pointer.Float64Ptr(math.Min(*expectedOcpRequirements.PacketLossPercentage, *details1.PacketLossPercentage))))
		},
		table.Entry("Worker", models.HostRoleWorker, models.ClusterHostRequirementsDetails{
			CPUCores:                         3,
			DiskSizeGb:                       122,
			RAMMib:                           9 * int64(units.KiB),
			InstallationDiskSpeedThresholdMs: 2,
			NetworkLatencyThresholdMs:        pointer.Float64Ptr(1000),
			PacketLossPercentage:             pointer.Float64Ptr(10),
		}),
		table.Entry("Master", models.HostRoleMaster, models.ClusterHostRequirementsDetails{
			CPUCores:                         5,
			DiskSizeGb:                       121,
			RAMMib:                           17 * int64(units.KiB),
			InstallationDiskSpeedThresholdMs: 1,
			NetworkLatencyThresholdMs:        pointer.Float64Ptr(100),
			PacketLossPercentage:             pointer.Float64Ptr(0),
		}),
	)
	table.DescribeTable("should contain correct requirements when no network latency or packet loss is defined in the OCP requirements",
		func(role models.HostRole, expectedOcpRequirements models.ClusterHostRequirementsDetails) {

			id1 := strfmt.UUID(uuid.New().String())
			host = &models.Host{ID: &id1, ClusterID: *cluster.ID, Role: role}
			cluster.OpenshiftVersion = "4.6"

			operatorsMock.EXPECT().GetRequirementsBreakdownForHostInCluster(gomock.Any(), gomock.Eq(cluster), gomock.Eq(host)).Return(operatorRequirements, nil)

			result, err := hwvalidator.GetClusterHostRequirements(context.TODO(), cluster, host)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			Expect(*result.Ocp).To(BeEquivalentTo(expectedOcpRequirements))

			Expect(result.Operators).To(ConsistOf(operatorRequirements))

			Expect(result.Total.DiskSizeGb).To(Equal(expectedOcpRequirements.DiskSizeGb + details1.DiskSizeGb + details2.DiskSizeGb))
			Expect(result.Total.CPUCores).To(Equal(expectedOcpRequirements.CPUCores + details1.CPUCores + details2.CPUCores))
			Expect(result.Total.RAMMib).To(Equal(expectedOcpRequirements.RAMMib + details1.RAMMib + details2.RAMMib))
			Expect(result.Total.InstallationDiskSpeedThresholdMs).To(Equal(details2.InstallationDiskSpeedThresholdMs))
			Expect(result.Total.NetworkLatencyThresholdMs).To(Equal(pointer.Float64Ptr(math.Min(*details1.NetworkLatencyThresholdMs, *details2.NetworkLatencyThresholdMs))))
			Expect(result.Total.PacketLossPercentage).To(Equal(details1.PacketLossPercentage))
		},
		table.Entry("Worker", models.HostRoleWorker, models.ClusterHostRequirementsDetails{
			CPUCores:                         2,
			DiskSizeGb:                       120,
			RAMMib:                           8 * int64(units.KiB),
			InstallationDiskSpeedThresholdMs: 0,
		}),
		table.Entry("Master", models.HostRoleMaster, models.ClusterHostRequirementsDetails{
			CPUCores:                         4,
			DiskSizeGb:                       120,
			RAMMib:                           16 * int64(units.KiB),
			InstallationDiskSpeedThresholdMs: 0,
		}),
	)
})

var _ = Describe("Preflight host requirements", func() {

	var (
		cfg         ValidatorCfg
		hwvalidator Validator
		cluster     *common.Cluster

		ctrl          *gomock.Controller
		operatorsMock *operators.MockAPI

		operatorRequirements []*models.OperatorHardwareRequirements
	)

	var versionRequirements = VersionedRequirementsDecoder{
		"default": {
			Version: "default",
			MasterRequirements: &models.ClusterHostRequirementsDetails{
				CPUCores:                         defaultMasterCores,
				RAMMib:                           defaultMasterRam,
				DiskSizeGb:                       defaultMasterDiskSize,
				InstallationDiskSpeedThresholdMs: defaultMasterDiskSpeedThreshold,
			},
			WorkerRequirements: &models.ClusterHostRequirementsDetails{
				CPUCores:                         defaultWorkerCores,
				RAMMib:                           defaultWorkerRam,
				DiskSizeGb:                       defaultWorkerDiskSize,
				InstallationDiskSpeedThresholdMs: defaultWorkerDiskSpeedThreshold,
			},
			SNORequirements: &models.ClusterHostRequirementsDetails{
				CPUCores:                         defaultSnoCores,
				RAMMib:                           defaultSnoRam,
				DiskSizeGb:                       defaultMasterDiskSize,
				InstallationDiskSpeedThresholdMs: defaultMasterDiskSpeedThreshold,
			},
		},
		"4.6": {
			Version: "4.6",
			MasterRequirements: &models.ClusterHostRequirementsDetails{
				CPUCores:   4,
				RAMMib:     16384,
				DiskSizeGb: 120,
			},
			WorkerRequirements: &models.ClusterHostRequirementsDetails{
				CPUCores:   2,
				RAMMib:     8192,
				DiskSizeGb: 120,
			},
			SNORequirements: &models.ClusterHostRequirementsDetails{
				CPUCores:   8,
				RAMMib:     32768,
				DiskSizeGb: 120,
			},
		},
		"4.7": {
			Version: "4.7",
			MasterRequirements: &models.ClusterHostRequirementsDetails{
				CPUCores:                         5,
				RAMMib:                           17408,
				DiskSizeGb:                       121,
				InstallationDiskSpeedThresholdMs: 1,
			},
			WorkerRequirements: &models.ClusterHostRequirementsDetails{
				CPUCores:                         3,
				RAMMib:                           9216,
				DiskSizeGb:                       122,
				InstallationDiskSpeedThresholdMs: 2,
			},
			SNORequirements: &models.ClusterHostRequirementsDetails{
				CPUCores:   7,
				RAMMib:     31744,
				DiskSizeGb: 123,
			},
		},
	}

	const (
		openShiftVersionNotInConfig = "4.5"
	)

	BeforeEach(func() {
		operatorName1 := "op-one"
		operatorName2 := "op-two"

		clusterID := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			OpenshiftVersion: openShiftVersionNotInConfig,
			MonitoredOperators: []*models.MonitoredOperator{
				{Name: operatorName1, ClusterID: clusterID},
				{Name: operatorName2, ClusterID: clusterID},
			},
		}}
		cfg.VersionedRequirements = versionRequirements

		operatorRequirements = []*models.OperatorHardwareRequirements{
			{OperatorName: operatorName1},
			{OperatorName: operatorName2},
		}

		ctrl = gomock.NewController(GinkgoT())
		operatorsMock = operators.NewMockAPI(ctrl)

		hwvalidator = NewValidator(logrus.New(), cfg, operatorsMock)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("should contain correct default preflight host requirements", func() {
		operatorsMock.EXPECT().GetPreflightRequirementsBreakdownForCluster(gomock.Any(), gomock.Eq(cluster)).Return(operatorRequirements, nil)

		result, err := hwvalidator.GetPreflightHardwareRequirements(context.TODO(), cluster)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())

		expectedOcpRequirements := models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores:                         defaultMasterCores,
					DiskSizeGb:                       defaultMasterDiskSize,
					RAMMib:                           defaultMasterRam,
					InstallationDiskSpeedThresholdMs: defaultMasterDiskSpeedThreshold,
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores:                         defaultWorkerCores,
					DiskSizeGb:                       defaultWorkerDiskSize,
					RAMMib:                           defaultWorkerRam,
					InstallationDiskSpeedThresholdMs: defaultWorkerDiskSpeedThreshold,
				},
			},
		}
		Expect(*result.Ocp).To(BeEquivalentTo(expectedOcpRequirements))
		Expect(result.Operators).To(ConsistOf(operatorRequirements))
	})

	It("should contain correct preflight  host requirements - single node", func() {
		cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
		operatorsMock.EXPECT().GetPreflightRequirementsBreakdownForCluster(gomock.Any(), gomock.Eq(cluster)).Return(operatorRequirements, nil)

		result, err := hwvalidator.GetPreflightHardwareRequirements(context.TODO(), cluster)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())

		expectedOcpRequirements := models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores:                         defaultSnoCores,
					DiskSizeGb:                       defaultMasterDiskSize,
					RAMMib:                           defaultSnoRam,
					InstallationDiskSpeedThresholdMs: defaultMasterDiskSpeedThreshold,
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores:                         defaultWorkerCores,
					DiskSizeGb:                       defaultWorkerDiskSize,
					RAMMib:                           defaultWorkerRam,
					InstallationDiskSpeedThresholdMs: defaultWorkerDiskSpeedThreshold,
				},
			},
		}
		Expect(*result.Ocp).To(BeEquivalentTo(expectedOcpRequirements))
		Expect(result.Operators).To(ConsistOf(operatorRequirements))
	})

	It("should fail providing on operator API error", func() {
		failure := errors.New("boom")
		operatorsMock.EXPECT().GetPreflightRequirementsBreakdownForCluster(gomock.Any(), gomock.Eq(cluster)).Return(nil, failure)

		_, err := hwvalidator.GetPreflightHardwareRequirements(context.TODO(), cluster)

		Expect(err).To(HaveOccurred())
		Expect(err).To(Equal(failure))
	})

	It("should contain correct preflight requirements for dedicated OCP version", func() {
		cluster.OpenshiftVersion = "4.7"
		operatorsMock.EXPECT().GetPreflightRequirementsBreakdownForCluster(gomock.Any(), gomock.Eq(cluster)).Return(operatorRequirements, nil)

		result, err := hwvalidator.GetPreflightHardwareRequirements(context.TODO(), cluster)

		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())

		expectedOcpRequirements := models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores:                         5,
					DiskSizeGb:                       121,
					RAMMib:                           17 * int64(units.KiB),
					InstallationDiskSpeedThresholdMs: 1,
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores:                         3,
					DiskSizeGb:                       122,
					RAMMib:                           9 * int64(units.KiB),
					InstallationDiskSpeedThresholdMs: 2,
				},
			},
		}
		Expect(*result.Ocp).To(BeEquivalentTo(expectedOcpRequirements))
		Expect(result.Operators).To(ConsistOf(operatorRequirements))
	})

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
