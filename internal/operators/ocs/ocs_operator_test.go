package ocs

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

var _ = Describe("Ocs Operator Test GetHostRequirements", func() {
	var (
		ctx      = context.TODO()
		operator = NewOcsOperator(common.GetTestLog())
		master1  = &models.Host{Role: models.HostRoleMaster,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD"},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD"},
				}})}
		master2 = &models.Host{Role: models.HostRoleMaster, Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32})}
		master3 = &models.Host{Role: models.HostRoleMaster,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD"}}})}
		worker1 = &models.Host{Role: models.HostRoleWorker,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64 * conversions.GB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD"},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD"},
				}})}
		worker2 = &models.Host{Role: models.HostRoleWorker,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64 * conversions.GB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD"},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD"},
					{SizeBytes: 40 * conversions.GB, DriveType: "HDD"},
				}})}
		worker3        = &models.Host{Role: models.HostRoleWorker, Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64})}
		autoAssignHost = &models.Host{Role: models.HostRoleAutoAssign, Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GB,
			Disks: []*models.Disk{
				{SizeBytes: 20 * conversions.GB, DriveType: "HDD"},
				{SizeBytes: 40 * conversions.GB, DriveType: "SSD"},
			}})}
	)

	Context("GetHostRequirements", func() {
		table.DescribeTable("passing scenario: get requirements for hosts when ", func(cluster *common.Cluster, host *models.Host, expectedResult *models.ClusterHostRequirementsDetails) {
			res, _ := operator.GetHostRequirements(ctx, cluster, host)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("there are three masters",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					master1, master2, master3,
				}}},
				master1,
				&models.ClusterHostRequirementsDetails{CPUCores: CPUCompactMode + operator.config.OCSRequiredDiskCPUCount, RAMMib: conversions.GbToMib(MemoryGBCompactMode + operator.config.OCSRequiredDiskRAMGB), DiskSizeGb: MinDiskSize},
			),
			table.Entry("there are 3 hosts, role of one as auto-assign",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					master1, master2, autoAssignHost,
				}}},
				autoAssignHost,
				&models.ClusterHostRequirementsDetails{CPUCores: CPUCompactMode + operator.config.OCSRequiredDiskCPUCount, RAMMib: conversions.GbToMib(MemoryGBCompactMode + operator.config.OCSRequiredDiskRAMGB), DiskSizeGb: MinDiskSize},
			),
			table.Entry("there are 6 hosts, master requirements",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					master1, master2, master3, worker1, worker2, worker3,
				}}},
				master1,
				&models.ClusterHostRequirementsDetails{CPUCores: 0, RAMMib: 0},
			),
			table.Entry("there are 6 hosts, worker with two disk requirements",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					master1, master2, master3, worker1, worker2, worker3,
				}}},
				worker1,
				&models.ClusterHostRequirementsDetails{CPUCores: CPUMinimalMode + operator.config.OCSRequiredDiskCPUCount, RAMMib: conversions.GbToMib(MemoryGBMinimalMode + operator.config.OCSRequiredDiskRAMGB), DiskSizeGb: MinDiskSize},
			),
			table.Entry("there are 6 hosts, worker with three disk requirements",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					master1, master2, master3, worker1, worker2, worker3,
				}}},
				worker2,
				&models.ClusterHostRequirementsDetails{CPUCores: CPUMinimalMode + 2*operator.config.OCSRequiredDiskCPUCount, RAMMib: conversions.GbToMib(MemoryGBMinimalMode + 2*operator.config.OCSRequiredDiskRAMGB), DiskSizeGb: MinDiskSize},
			),
			table.Entry("there are 6 hosts, worker with one disk requirements",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					master1, master2, master3, worker1, worker2, worker3,
				}}},
				worker3,
				&models.ClusterHostRequirementsDetails{CPUCores: CPUMinimalMode, RAMMib: conversions.GbToMib(MemoryGBMinimalMode)},
			),
		)
		table.DescribeTable("failure scenario: get requirements for hosts when ", func(cluster *common.Cluster, host *models.Host, expectedResult error) {
			_, err := operator.GetHostRequirements(ctx, cluster, host)
			Expect(err).To(BeEquivalentTo(expectedResult))
		},
			table.Entry("Single master",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					master1,
				}}},
				master1,
				fmt.Errorf("OCS requires a minimum of 3 hosts"),
			),
			table.Entry("there are two master",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					master1, master2,
				}}},
				master1,
				fmt.Errorf("OCS requires a minimum of 3 hosts"),
			),
			table.Entry("no disk in one of the master",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					master1, master2, master3,
				}}},
				master2,
				fmt.Errorf("OCS requires a minimum of one non-bootable disk per host"),
			),
			table.Entry("only one disk in one of the master",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					master1, master2, master3,
				}}},
				master3,
				fmt.Errorf("OCS requires a minimum of one non-bootable disk per host"),
			),
			table.Entry("there are two master and one worker",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					master1, master2, worker1,
				}}},
				worker1,
				fmt.Errorf("OCS compact mode unsupported role: %s", worker1.Role),
			),
			table.Entry("there are 4 hosts, role of one as auto-assign",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					master1, master2, autoAssignHost, master3,
				}}},
				autoAssignHost,
				fmt.Errorf("OCS minimal mode unsupported role: %s", autoAssignHost.Role),
			),
		)
	})
})
