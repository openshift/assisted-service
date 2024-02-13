package cnv_test

import (
	"context"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
)

var _ = Describe("CNV operator", func() {
	var (
		log      = logrus.New()
		operator api.Operator
	)

	BeforeEach(func() {
		cfg := cnv.Config{
			SupportedGPUs: map[string]bool{
				"10de:1db6": true,
				"10de:1eb8": true,
			},
			SupportedSRIOVNetworkIC: map[string]bool{
				"8086:158b": true,
				"15b3:1015": true,
			}}
		operator = cnv.NewCNVOperator(log, cfg, nil)
	})

	Context("getDependencies", func() {
		It("request for lvmo", func() {
			haMode := models.ClusterHighAvailabilityModeNone
			cluster := common.Cluster{
				Cluster: models.Cluster{HighAvailabilityMode: &haMode, OpenshiftVersion: lvm.LvmsMinOpenshiftVersion4_12},
			}

			requirements, err := operator.GetDependencies(&cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			Expect(requirements[0]).To(BeEquivalentTo(lvm.Operator.Name))
		})

		It("request for lso, ocp version older than 4.11 will not get lvmo", func() {
			haMode := models.ClusterHighAvailabilityModeNone
			cluster := common.Cluster{
				Cluster: models.Cluster{HighAvailabilityMode: &haMode, OpenshiftVersion: "4.11.0-0.0"},
			}

			requirements, err := operator.GetDependencies(&cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			Expect(requirements[0]).To(BeEquivalentTo(lso.Operator.Name))
		})
	})

	Context("host requirements", func() {

		var cluster common.Cluster

		BeforeEach(func() {
			mode := models.ClusterHighAvailabilityModeFull
			cluster = common.Cluster{
				Cluster: models.Cluster{HighAvailabilityMode: &mode, OpenshiftVersion: lvm.LvmsMinOpenshiftVersion4_12},
			}
		})

		table.DescribeTable("should be returned for no inventory", func(role models.HostRole, expectedRequirements *models.ClusterHostRequirementsDetails) {
			host := models.Host{Role: role}

			requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			Expect(requirements).To(BeEquivalentTo(expectedRequirements))
		},
			table.Entry("for master", models.HostRoleMaster, newRequirements(cnv.MasterCPU, cnv.MasterMemory)),
			table.Entry("for worker", models.HostRoleWorker, newRequirements(cnv.WorkerCPU, cnv.WorkerMemory)),
		)

		table.DescribeTable("should be returned for worker inventory with supported GPUs",
			func(gpus []*models.Gpu, expectedRequirements *models.ClusterHostRequirementsDetails) {
				host := models.Host{
					Role:      models.HostRoleWorker,
					Inventory: getInventoryWithGPUs(gpus),
				}

				requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

				Expect(err).ToNot(HaveOccurred())
				Expect(requirements).ToNot(BeNil())
				Expect(requirements).To(BeEquivalentTo(expectedRequirements))
			},
			table.Entry("1 supported GPU",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+1024)),
			table.Entry("1 supported GPU+1 unsupported",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1111", VendorID: "0000"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+1024)),

			table.Entry("2 supported GPUs",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1eb8", VendorID: "10de"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+2*1024)),
			table.Entry("3 identical supported GPUs",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1db6", VendorID: "10de"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+3*1024)),

			table.Entry("2 unsupported GPUs only",
				[]*models.Gpu{{DeviceID: "2222", VendorID: "0000"}, {DeviceID: "1111", VendorID: "0000"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory)),
		)

		table.DescribeTable("should be returned for worker inventory with supported SR-IOV interfaces",
			func(interfaces []*models.Interface, expectedRequirements *models.ClusterHostRequirementsDetails) {
				host := models.Host{
					Role:      models.HostRoleWorker,
					Inventory: getInventoryWithInterfaces(interfaces),
				}

				requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

				Expect(err).ToNot(HaveOccurred())
				Expect(requirements).ToNot(BeNil())
				Expect(requirements).To(BeEquivalentTo(expectedRequirements))
			},
			table.Entry("1 supported SR-IOV Interface",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+1024)),
			table.Entry("1 supported SR-IOV Interface+1 unsupported",
				[]*models.Interface{{Product: "0x158B", Vendor: "0x8086"}, {Product: "1111", Vendor: "0000"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+1024)),

			table.Entry("2 supported SR-IOV Interfaces",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}, {Product: "1015", Vendor: "15b3"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+2*1024)),
			table.Entry("3 identical supported SR-IOV Interfaces",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}, {Product: "0x158b", Vendor: "0x8086"}, {Product: "0x158b", Vendor: "0x8086"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+3*1024)),

			table.Entry("2 unsupported SR-IOV Interfaces only",
				[]*models.Interface{{Product: "2222", Vendor: "0000"}, {Product: "1111", Vendor: "0000"}},
				newRequirements(cnv.WorkerCPU, cnv.WorkerMemory)),
		)

		table.DescribeTable("should be returned for master inventory with GPUs", func(gpus []*models.Gpu) {
			host := models.Host{
				Role:      models.HostRoleMaster,
				Inventory: getInventoryWithGPUs(gpus),
			}

			requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			Expect(requirements).To(BeEquivalentTo(newRequirements(cnv.MasterCPU, cnv.MasterMemory)))
		},
			table.Entry("1 supported GPU",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}}),

			table.Entry("2 supported GPUs",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1eb8", VendorID: "10de"}}),

			table.Entry("3 identical supported GPUs",
				[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1db6", VendorID: "10de"}, {DeviceID: "1db6", VendorID: "10de"}}),

			table.Entry("1 unsupported GPU",
				[]*models.Gpu{{DeviceID: "1111", VendorID: "0000"}}),
			table.Entry("2 unsupported GPUs only",
				[]*models.Gpu{{DeviceID: "2222", VendorID: "0000"}, {DeviceID: "1111", VendorID: "0000"}}),
		)

		table.DescribeTable("should be returned for master inventory with SR-IOV interfaces",
			func(interfaces []*models.Interface) {
				host := models.Host{
					Role:      models.HostRoleMaster,
					Inventory: getInventoryWithInterfaces(interfaces),
				}

				requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

				Expect(err).ToNot(HaveOccurred())
				Expect(requirements).ToNot(BeNil())
				Expect(requirements).To(BeEquivalentTo(newRequirements(cnv.MasterCPU, cnv.MasterMemory)))
			},
			table.Entry("1 supported SR-IOV Interface",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}}),
			table.Entry("1 supported SR-IOV Interface+1 unsupported",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}, {Product: "1111", Vendor: "0000"}}),

			table.Entry("2 supported SR-IOV Interfaces",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}, {Product: "0x1015", Vendor: "0x15b3"}}),
			table.Entry("3 identical supported SR-IOV Interfaces",
				[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}, {Product: "0x158b", Vendor: "0x8086"}, {Product: "0x158b", Vendor: "0x8086"}}),

			table.Entry("2 unsupported SR-IOV Interfaces only",
				[]*models.Interface{{Product: "2222", Vendor: "0000"}, {Product: "1111", Vendor: "0000"}}),
		)

		It("should be returned for worker with supported GPU and SR-IOV interface", func() {
			host := models.Host{
				Role: models.HostRoleWorker,
				Inventory: getInventoryWith(
					[]*models.Gpu{{DeviceID: "1db6", VendorID: "10de"}},
					[]*models.Interface{{Product: "0x158b", Vendor: "0x8086"}},
				),
			}

			requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			Expect(requirements).To(BeEquivalentTo(newRequirements(cnv.WorkerCPU, cnv.WorkerMemory+2*1024)))
		})

		It("should fail for worker with malformed inventory JSON", func() {
			host := models.Host{
				Role:      models.HostRoleWorker,
				Inventory: "garbage...garbage...trash",
			}

			_, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

			Expect(err).To(HaveOccurred())
		})

		It("should return reqs for SNO", func() {
			host := models.Host{Role: models.HostRoleMaster}
			haMode := models.ClusterHighAvailabilityModeNone
			cluster = common.Cluster{
				Cluster: models.Cluster{HighAvailabilityMode: &haMode, OpenshiftVersion: lvm.LvmsMinOpenshiftVersion4_12},
			}

			requirements, err := operator.GetHostRequirements(context.TODO(), &cluster, &host)

			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			Expect(requirements).To(BeEquivalentTo(newRequirements(cnv.WorkerCPU+cnv.MasterCPU, cnv.WorkerMemory+cnv.MasterMemory)))
		})
	})

	Context("ValidateHost", func() {
		cfg := cnv.Config{
			SupportedGPUs: map[string]bool{
				"10de:1db6": true,
				"10de:1eb8": true,
			},
			SupportedSRIOVNetworkIC: map[string]bool{
				"8086:158b": true,
				"15b3:1015": true,
			},
			SNOPoolSizeRequestHPPGib: 50,
			SNOInstallHPP:            true,
		}
		cnvOperator := cnv.NewCNVOperator(log, cfg, nil)
		fullHaMode := models.ClusterHighAvailabilityModeFull
		noneHaMode := models.ClusterHighAvailabilityModeNone
		masterWithLessDiskSizeAndVirt := &models.Host{Role: models.HostRoleMaster, InstallationDiskID: "disk1",
			Inventory: getInventoryWithCpuFlagsAndDisks([]string{"vmx"}, []*models.Disk{
				{SizeBytes: 20 * conversions.GiB, DriveType: models.DriveTypeHDD, ID: "disk1"},
				{SizeBytes: 40 * conversions.GiB, DriveType: models.DriveTypeSSD, ID: "disk2"},
				{SizeBytes: 20 * conversions.GiB, DriveType: models.DriveTypeSSD, ID: "disk3"},
			})}
		masterWithOneSatisfyingDiskAndVirt := &models.Host{Role: models.HostRoleMaster, InstallationDiskID: "disk1",
			Inventory: getInventoryWithCpuFlagsAndDisks([]string{"vmx"}, []*models.Disk{
				{SizeBytes: 20 * conversions.GiB, DriveType: models.DriveTypeHDD, ID: "disk1"},
				{SizeBytes: 60 * conversions.GiB, DriveType: models.DriveTypeSSD, ID: "disk2"},
				{SizeBytes: 20 * conversions.GiB, DriveType: models.DriveTypeSSD, ID: "disk3"},
			})}
		masterWithoutVirt := &models.Host{Role: models.HostRoleMaster, InstallationDiskID: "disk1",
			Inventory: getInventoryWithCpuFlagsAndDisks([]string{}, []*models.Disk{
				{SizeBytes: 20 * conversions.GiB, DriveType: models.DriveTypeHDD, ID: "disk1"},
				{SizeBytes: 40 * conversions.GiB, DriveType: models.DriveTypeSSD, ID: "disk2"},
				{SizeBytes: 20 * conversions.GiB, DriveType: models.DriveTypeSSD, ID: "disk3"},
			})}
		table.DescribeTable("validateHost when ", func(cluster *common.Cluster, host *models.Host, expectedResult api.ValidationResult) {
			res, _ := cnvOperator.ValidateHost(context.TODO(), cluster, host)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("No virt capabilities",
				&common.Cluster{Cluster: models.Cluster{OpenshiftVersion: "4.10", Hosts: []*models.Host{masterWithoutVirt}}},
				masterWithoutVirt,
				api.ValidationResult{Status: api.Failure, ValidationId: cnvOperator.GetHostValidationID(), Reasons: []string{"CPU does not have virtualization support"}},
			),
			table.Entry("SNO and there is no disk with bigger size than threshold for HPP",
				&common.Cluster{Cluster: models.Cluster{OpenshiftVersion: "4.10", HighAvailabilityMode: &noneHaMode, Hosts: []*models.Host{masterWithLessDiskSizeAndVirt}}},
				masterWithLessDiskSizeAndVirt,
				api.ValidationResult{Status: api.Failure, ValidationId: cnvOperator.GetHostValidationID(), Reasons: []string{"OpenShift Virtualization on SNO requires an additional disk with 53 GB (50 Gi) in order to provide persistent storage for VMs, using hostpath-provisioner"}},
			),
			table.Entry("SNO and there is a disk with bigger size than threshold for HPP",
				&common.Cluster{Cluster: models.Cluster{OpenshiftVersion: "4.10", HighAvailabilityMode: &noneHaMode, Hosts: []*models.Host{masterWithOneSatisfyingDiskAndVirt}}},
				masterWithOneSatisfyingDiskAndVirt,
				api.ValidationResult{Status: api.Success, ValidationId: cnvOperator.GetHostValidationID(), Reasons: nil},
			),
			table.Entry("Non SNO and there is no disk with bigger size than threshold for HPP shouldn't bother us",
				&common.Cluster{Cluster: models.Cluster{OpenshiftVersion: "4.10", HighAvailabilityMode: &fullHaMode, Hosts: []*models.Host{masterWithLessDiskSizeAndVirt}}},
				masterWithLessDiskSizeAndVirt,
				api.ValidationResult{Status: api.Success, ValidationId: cnvOperator.GetHostValidationID(), Reasons: nil},
			),
		)
	})

	Context("preflight hardware requirements", func() {
		fullHaMode := models.ClusterHighAvailabilityModeFull
		noneHaMode := models.ClusterHighAvailabilityModeNone

		table.DescribeTable("should be returned", func(cfg cnv.Config, cluster common.Cluster) {
			cnvOperator := cnv.NewCNVOperator(log, cfg, nil)
			requirements, err := cnvOperator.GetPreflightRequirements(context.TODO(), &cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(requirements.Dependencies).To(ConsistOf(lso.Operator.Name))
			Expect(requirements.OperatorName).To(BeEquivalentTo(cnv.Operator.Name))
			numQualitative := 3
			workerRequirements := newRequirements(cnv.WorkerCPU, cnv.WorkerMemory)
			masterRequirements := newRequirements(cnv.MasterCPU, cnv.MasterMemory)

			if common.IsSingleNodeCluster(&cluster) {
				// CNV+SNO installs HPP storage; additional discoverable disk req
				if cfg.SNOInstallHPP {
					numQualitative += 1
				}
				masterRequirements = newRequirements(cnv.MasterCPU+cnv.WorkerCPU, cnv.MasterMemory+cnv.WorkerMemory)
			}

			Expect(requirements.Requirements.Worker.Qualitative).To(HaveLen(numQualitative))
			Expect(requirements.Requirements.Worker.Quantitative).To(BeEquivalentTo(workerRequirements))

			Expect(requirements.Requirements.Master.Qualitative).To(HaveLen(numQualitative))
			Expect(requirements.Requirements.Master.Quantitative).To(BeEquivalentTo(masterRequirements))

			Expect(requirements.Requirements.Master.Qualitative).To(BeEquivalentTo(requirements.Requirements.Worker.Qualitative))
		},
			table.Entry("for non-SNO", cnv.Config{SNOPoolSizeRequestHPPGib: 50}, common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:     "4.10",
				HighAvailabilityMode: &fullHaMode,
			}}),
			table.Entry("for SNO", cnv.Config{SNOPoolSizeRequestHPPGib: 50, SNOInstallHPP: true}, common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:     "4.10",
				HighAvailabilityMode: &noneHaMode,
			}}),
			table.Entry("for SNO and opt out of HPP via env var", cnv.Config{SNOPoolSizeRequestHPPGib: 50, SNOInstallHPP: false}, common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:     "4.10",
				HighAvailabilityMode: &noneHaMode,
			}}),
		)
	})

	Context("cluster requirements", func() {
		It("only x86_64 is supported for CNV operator", func() {
			cluster := common.Cluster{}

			cluster.CPUArchitecture = common.DefaultCPUArchitecture
			validation, err := operator.ValidateCluster(context.TODO(), &cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(validation.Status).To(Equal(api.Success))

			cluster.CPUArchitecture = "arm64"
			validation, err = operator.ValidateCluster(context.TODO(), &cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(validation.Status).To(Equal(api.Failure))
			Expect(validation.Reasons).To(ContainElements(
				"OpenShift Virtualization is supported only for x86_64 CPU architecture."))
		})
		It("multi-arch is supported for CNV operator", func() {
			cluster := common.Cluster{}

			cluster.CPUArchitecture = common.MultiCPUArchitecture
			validation, err := operator.ValidateCluster(context.TODO(), &cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(validation.Status).To(Equal(api.Success))
		})
	})
})

func getInventoryWithCpuFlagsAndDisks(flags []string, disks []*models.Disk) string {
	inventory := models.Inventory{
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
				IPV6Addresses: []string{
					"1001:db8::10/120",
				},
			},
		},
		CPU: &models.CPU{
			Count: 12,
			Flags: flags,
		},
		Memory: &models.Memory{
			UsableBytes: 32 * conversions.GiB,
		},
		Disks: disks,
	}
	return marshal(inventory)
}

func getInventoryWithGPUs(gpus []*models.Gpu) string {
	inventory := models.Inventory{Gpus: gpus}
	return marshal(inventory)
}

func getInventoryWithInterfaces(interfaces []*models.Interface) string {
	inventory := models.Inventory{Interfaces: interfaces}
	return marshal(inventory)
}

func getInventoryWith(gpus []*models.Gpu, interfaces []*models.Interface) string {
	inventory := models.Inventory{Gpus: gpus, Interfaces: interfaces}
	return marshal(inventory)
}

func marshal(inventory models.Inventory) string {
	inventoryJSON, err := common.MarshalInventory(&inventory)
	Expect(err).ToNot(HaveOccurred())
	return inventoryJSON
}

func newRequirements(cpuCores int64, ramMib int64) *models.ClusterHostRequirementsDetails {
	return &models.ClusterHostRequirementsDetails{CPUCores: cpuCores, RAMMib: ramMib}
}
