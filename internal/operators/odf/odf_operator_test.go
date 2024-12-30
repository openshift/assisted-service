package odf

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

var (
	diskID1   = "/dev/disk/by-id/test-disk-1"
	diskID2   = "/dev/disk/by-id/test-disk-2"
	diskID3   = "/dev/disk/by-id/test-disk-3"
	clusterID = strfmt.UUID(uuid.New().String())
)

func getHostID() *strfmt.UUID {
	id := strfmt.UUID(uuid.New().String())
	return &id
}

var _ = Describe("Odf Operator", func() {
	var (
		ctx                 = context.TODO()
		operator            = NewOdfOperator(common.GetTestLog())
		masterWithThreeDisk = &models.Host{ID: getHostID(), Role: models.HostRoleMaster, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID2},
					{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID3},
				}})}
		masterWithThreeDiskSizeOfOneZero = &models.Host{ID: getHostID(), Role: models.HostRoleMaster, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID2},
					{SizeBytes: 0 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID3},
				}})}
		masterWithNoDisk           = &models.Host{ID: getHostID(), Role: models.HostRoleMaster, Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB})}
		masterWithNoInventory      = &models.Host{ID: getHostID(), Role: models.HostRoleMaster}
		masterWithInvalidInventory = &models.Host{ID: getHostID(), Role: models.HostRoleMaster, Inventory: "invalid"}
		masterWithOneDisk          = &models.Host{ID: getHostID(), Role: models.HostRoleMaster, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1}}})}
		masterWithTwoSmallDisk = &models.Host{ID: getHostID(), Role: models.HostRoleMaster, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID1},
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}})}
		masterWithLessDiskSize = &models.Host{ID: getHostID(), Role: models.HostRoleMaster, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID2},
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID3},
				}})}
		workerWithTwoDisk = &models.Host{ID: getHostID(), Role: models.HostRoleWorker, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID2},
				}})}
		workerWithThreeDisk = &models.Host{ID: getHostID(), Role: models.HostRoleWorker, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID2},
					{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID3},
				}})}
		workerWithThreeDiskSizeOfOneZero = &models.Host{ID: getHostID(), Role: models.HostRoleWorker, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID2},
					{SizeBytes: 0 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID3},
				}})}
		workerWithNoDisk           = &models.Host{ID: getHostID(), Role: models.HostRoleWorker, Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB})}
		workerWithNoInventory      = &models.Host{ID: getHostID(), Role: models.HostRoleWorker}
		workerWithInvalidInventory = &models.Host{ID: getHostID(), Role: models.HostRoleWorker, Inventory: "invalid"}
		workerWithLessDiskSize     = &models.Host{ID: getHostID(), Role: models.HostRoleWorker, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID2},
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID3},
				}})}
		workerWithTwoSmallDisk = &models.Host{ID: getHostID(), Role: models.HostRoleWorker, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID1},
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}})}
		autoAssignHost = &models.Host{ID: getHostID(), Role: models.HostRoleAutoAssign, SuggestedRole: models.HostRoleAutoAssign, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID2},
				}})}
	)

	Context("GetHostRequirements", func() {
		table.DescribeTable("unknown mode scenario: get requirements for hosts when ", func(cluster *common.Cluster, host *models.Host, expectedResult *models.ClusterHostRequirementsDetails) {
			res, _ := operator.GetHostRequirements(ctx, cluster, host)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("there is a single master",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
				}}},
				masterWithThreeDisk,
				&models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			),

			table.Entry("there are two masters and one worker",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithLessDiskSize,
					masterWithOneDisk,
					workerWithInvalidInventory,
				}}},
				masterWithOneDisk,
				&models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			),

			table.Entry("there are 3 masters and 2 workers",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithLessDiskSize,
					masterWithOneDisk,
					workerWithInvalidInventory,
					workerWithInvalidInventory,
				}}},
				masterWithThreeDisk,
				&models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			),

			table.Entry("there are 4 masters and 2 workers",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithInvalidInventory,
					masterWithLessDiskSize,
					masterWithOneDisk,
					workerWithInvalidInventory,
					workerWithInvalidInventory,
				}}},
				masterWithThreeDisk,
				&models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			),

			table.Entry("missing inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithNoInventory,
				}}},
				masterWithNoInventory,
				&models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			),

			table.Entry("invalid inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithInvalidInventory,
				}}},
				masterWithInvalidInventory,
				&models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			),
		)

		table.DescribeTable("compact mode scenario: get requirements for hosts when ", func(cluster *common.Cluster, host *models.Host, expectedResult *models.ClusterHostRequirementsDetails) {
			res, _ := operator.GetHostRequirements(ctx, cluster, host)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("there are 2 masters and 1 auto-assign - requirements for auto-assign",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithInvalidInventory, masterWithLessDiskSize, autoAssignHost,
				}}},
				autoAssignHost,
				&models.ClusterHostRequirementsDetails{
					CPUCores: operator.config.ODFPerHostCPUCompactMode + 1*operator.config.ODFPerDiskCPUCount,
					RAMMib:   conversions.GibToMib(operator.config.ODFPerHostMemoryGiBCompactMode + 1*operator.config.ODFPerDiskRAMGiB),
				},
			),

			table.Entry("there is a master with invalid inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithInvalidInventory, masterWithLessDiskSize, autoAssignHost,
				}}},
				masterWithInvalidInventory,
				nil,
			),

			table.Entry("there are 3 masters - reuqirements for 2 eligible disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDiskSizeOfOneZero, masterWithNoDisk, masterWithThreeDisk,
				}}},
				masterWithThreeDisk,
				&models.ClusterHostRequirementsDetails{
					CPUCores: operator.config.ODFPerHostCPUCompactMode + 2*operator.config.ODFPerDiskCPUCount,
					RAMMib:   conversions.GibToMib(operator.config.ODFPerHostMemoryGiBCompactMode + 2*operator.config.ODFPerDiskRAMGiB),
				},
			),

			table.Entry("there are 4 masters - reuqirements for 1 eligible disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDiskSizeOfOneZero, masterWithNoDisk, masterWithThreeDisk, masterWithLessDiskSize,
				}}},
				masterWithThreeDiskSizeOfOneZero,
				&models.ClusterHostRequirementsDetails{
					CPUCores: operator.config.ODFPerHostCPUCompactMode + 1*operator.config.ODFPerDiskCPUCount,
					RAMMib:   conversions.GibToMib(operator.config.ODFPerHostMemoryGiBCompactMode + 1*operator.config.ODFPerDiskRAMGiB),
				},
			),

			// in compact mode one disk is still considered for CPU / memory requirements
			table.Entry("there are 5 masters - reuqirements for 0 eligible disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDiskSizeOfOneZero, masterWithOneDisk, masterWithThreeDisk, masterWithLessDiskSize,
				}}},
				masterWithOneDisk,
				&models.ClusterHostRequirementsDetails{
					CPUCores: operator.config.ODFPerHostCPUCompactMode + 1*operator.config.ODFPerDiskCPUCount,
					RAMMib:   conversions.GibToMib(operator.config.ODFPerHostMemoryGiBCompactMode + 1*operator.config.ODFPerDiskRAMGiB),
				},
			),
		)

		table.DescribeTable("standard mode scenario: get requirements for hosts when ", func(cluster *common.Cluster, host *models.Host, expectedResult *models.ClusterHostRequirementsDetails) {
			res, _ := operator.GetHostRequirements(ctx, cluster, host)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("there are 3 masters, 3 workers and 1 auto-assign - requirements for auto-assign",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithNoDisk,
					masterWithInvalidInventory,
					workerWithInvalidInventory,
					workerWithLessDiskSize,
					workerWithNoDisk,
					autoAssignHost,
				}}},
				autoAssignHost,
				&models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			),

			table.Entry("there is a master with invalid inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithNoDisk,
					masterWithInvalidInventory,
					workerWithInvalidInventory,
					workerWithLessDiskSize,
					workerWithNoDisk,
				}}},
				masterWithInvalidInventory,
				&models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			),

			table.Entry("there is a worker with invalid inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithNoDisk,
					masterWithInvalidInventory,
					workerWithInvalidInventory,
					workerWithLessDiskSize,
					workerWithNoDisk,
				}}},
				workerWithInvalidInventory,
				nil,
			),

			table.Entry("there is a master with 0 eligible disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithNoDisk,
					masterWithInvalidInventory,
					workerWithInvalidInventory,
					workerWithLessDiskSize,
					workerWithNoDisk,
				}}},
				masterWithNoDisk,
				&models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			),

			table.Entry("there is a worker with 0 eligible disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithNoDisk,
					masterWithInvalidInventory,
					workerWithInvalidInventory,
					workerWithLessDiskSize,
					workerWithNoDisk,
				}}},
				workerWithNoDisk,
				&models.ClusterHostRequirementsDetails{
					CPUCores: operator.config.ODFPerHostCPUStandardMode + 0*operator.config.ODFPerDiskCPUCount,
					RAMMib:   conversions.GibToMib(operator.config.ODFPerHostMemoryGiBStandardMode + 0*operator.config.ODFPerDiskRAMGiB),
				},
			),

			table.Entry("there is a master with 1 eligible disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDiskSizeOfOneZero,
					masterWithInvalidInventory,
					workerWithInvalidInventory,
					workerWithLessDiskSize,
					workerWithNoDisk,
				}}},
				masterWithThreeDiskSizeOfOneZero,
				&models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			),

			table.Entry("there is a worker with 1 eligible disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithNoDisk,
					masterWithInvalidInventory,
					workerWithTwoDisk,
					workerWithLessDiskSize,
					workerWithNoDisk,
				}}},
				workerWithTwoDisk,
				&models.ClusterHostRequirementsDetails{
					CPUCores: operator.config.ODFPerHostCPUStandardMode + 1*operator.config.ODFPerDiskCPUCount,
					RAMMib:   conversions.GibToMib(operator.config.ODFPerHostMemoryGiBStandardMode + 1*operator.config.ODFPerDiskRAMGiB),
				},
			),

			table.Entry("there is a master with 2 eligible disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDiskSizeOfOneZero,
					masterWithInvalidInventory,
					workerWithInvalidInventory,
					workerWithLessDiskSize,
					workerWithNoDisk,
				}}},
				masterWithThreeDiskSizeOfOneZero,
				&models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			),

			table.Entry("there is a worker with 2 eligible disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithNoDisk,
					masterWithInvalidInventory,
					workerWithThreeDisk,
					workerWithLessDiskSize,
					workerWithNoDisk,
				}}},
				workerWithThreeDisk,
				&models.ClusterHostRequirementsDetails{
					CPUCores: operator.config.ODFPerHostCPUStandardMode + 2*operator.config.ODFPerDiskCPUCount,
					RAMMib:   conversions.GibToMib(operator.config.ODFPerHostMemoryGiBStandardMode + 2*operator.config.ODFPerDiskRAMGiB),
				},
			),
		)
	})

	Context("ValidateHost", func() {
		table.DescribeTable("unknown mode scenario: validateHost when ", func(cluster *common.Cluster, host *models.Host, expectedResult api.ValidationResult) {
			res, _ := operator.ValidateHost(ctx, cluster, host, nil)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("there is a single master",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
				}}},
				masterWithThreeDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("there is a single auto-assign",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					autoAssignHost,
				}}},
				autoAssignHost,
				api.ValidationResult{
					Status:       api.Pending,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Auto-assigning roles for hosts with ODF is allowed only for clusters with exactly three hosts. For other scenarios, please manually assign the host role as either a control plane node or a worker."},
				},
			),

			table.Entry("there are two masters and one worker",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, workerWithTwoDisk,
				}}},
				workerWithTwoDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("there are 3 masters and 2 workers",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithLessDiskSize, workerWithTwoDisk, workerWithInvalidInventory,
				}}},
				workerWithTwoDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("there are 4 masters and 2 workers",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithLessDiskSize, masterWithOneDisk, workerWithTwoDisk, workerWithInvalidInventory,
				}}},
				workerWithTwoDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("missing inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithLessDiskSize, masterWithOneDisk, workerWithTwoDisk, masterWithNoInventory,
				}}},
				masterWithNoInventory,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("invalid inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithLessDiskSize, masterWithOneDisk, workerWithTwoDisk, workerWithInvalidInventory,
				}}},
				workerWithInvalidInventory,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),
		)

		table.DescribeTable("compact mode scenario: validateHost when ", func(cluster *common.Cluster, host *models.Host, expectedResult api.ValidationResult) {
			res, _ := operator.ValidateHost(ctx, cluster, host, nil)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("there are 2 masters and 1 auto-assign - validate auto-assign",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithLessDiskSize, autoAssignHost,
				}}},
				autoAssignHost,
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{},
				},
			),

			table.Entry("there are 3 masters and 1 auto-assign - validate auto-assign",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithLessDiskSize, masterWithInvalidInventory, autoAssignHost,
				}}},
				autoAssignHost,
				api.ValidationResult{
					Status:       api.Pending,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Auto-assigning roles for hosts with ODF is allowed only for clusters with exactly three hosts. For other scenarios, please manually assign the host role as either a control plane node or a worker."},
				},
			),

			table.Entry("there is a master with missing inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithLessDiskSize, masterWithNoInventory,
				}}},
				masterWithNoInventory,
				api.ValidationResult{
					Status:       api.Pending,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Missing Inventory in the host."},
				},
			),

			table.Entry("there is a master with invalid inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithLessDiskSize, masterWithInvalidInventory,
				}}},
				masterWithInvalidInventory,
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Failed to get inventory from host."},
				},
			),

			table.Entry("there are 3 masters",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithOneDisk,
				}}},
				masterWithThreeDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("there are 4 masters",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithOneDisk, masterWithLessDiskSize,
				}}},
				masterWithThreeDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("there are 5 masters",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithOneDisk, masterWithLessDiskSize, masterWithNoDisk,
				}}},
				masterWithThreeDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("there is a master with no disk",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithOneDisk,
				}}},
				masterWithNoDisk,
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Insufficient disks, ODF requires at least one non-installation SSD or HDD disk on each host in compact mode."},
				},
			),

			table.Entry("there is a master with only one disk",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithOneDisk,
				}}},
				masterWithOneDisk,
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Insufficient disks, ODF requires at least one non-installation SSD or HDD disk on each host in compact mode."},
				},
			),

			table.Entry("there is a master with disk of size zero",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithThreeDiskSizeOfOneZero,
				}}},
				masterWithThreeDiskSizeOfOneZero,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("there is a master with 3 disks, one is too small",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithLessDiskSize,
				}}},
				masterWithLessDiskSize,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("there is a master with 2 small disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithTwoSmallDisk,
				}}},
				masterWithTwoSmallDisk,
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Insufficient resources to deploy ODF in compact mode. ODF requires a minimum of 3 hosts. Each host must have at least 1 additional disk of 25 GB minimum and an installation disk."}},
			),
		)

		table.DescribeTable("standard mode scenario: validateHosts when ", func(cluster *common.Cluster, host *models.Host, expectedResult api.ValidationResult) {
			res, _ := operator.ValidateHost(ctx, cluster, host, nil)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("there are 3 masters, 3 workers and 1 auto-assign - validate auto-assign",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithNoDisk,
					masterWithInvalidInventory,
					workerWithInvalidInventory,
					workerWithLessDiskSize,
					workerWithNoDisk,
					autoAssignHost,
				}}},
				autoAssignHost,
				api.ValidationResult{
					Status:       api.Pending,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Auto-assigning roles for hosts with ODF is allowed only for clusters with exactly three hosts. For other scenarios, please manually assign the host role as either a control plane node or a worker."},
				},
			),

			table.Entry("there is a master with missing inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithNoDisk,
					masterWithNoInventory,
					workerWithInvalidInventory,
					workerWithLessDiskSize,
					workerWithNoDisk,
				}}},
				masterWithNoInventory,
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{},
				},
			),

			table.Entry("there is a worker with missing inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithNoDisk,
					masterWithNoInventory,
					workerWithNoInventory,
					workerWithLessDiskSize,
					workerWithNoDisk,
				}}},
				workerWithNoInventory,
				api.ValidationResult{
					Status:       api.Pending,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Missing Inventory in the host."},
				},
			),

			table.Entry("there is a master with invalid inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithNoDisk,
					masterWithInvalidInventory,
					workerWithInvalidInventory,
					workerWithLessDiskSize,
					workerWithNoDisk,
				}}},
				masterWithInvalidInventory,
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{},
				},
			),

			table.Entry("there is a worker with invalid inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithNoDisk,
					masterWithNoInventory,
					workerWithInvalidInventory,
					workerWithLessDiskSize,
					workerWithNoDisk,
				}}},
				workerWithInvalidInventory,
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Failed to get inventory from host."},
				},
			),

			table.Entry("there is a master with with no disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithOneDisk, workerWithTwoDisk, workerWithThreeDisk, workerWithNoDisk,
				}}},
				masterWithNoDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("there is a worker with no disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithOneDisk, workerWithTwoDisk, workerWithThreeDisk, workerWithNoDisk,
				}}},
				workerWithNoDisk,
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons: []string{fmt.Sprintf(
						"Insufficient disks, ODF requires at least one non-installation SSD or HDD disk on each host in %s mode.",
						strings.ToLower(string(standardMode)),
					)},
				},
			),

			table.Entry("there is a master with 3 disks, one too small",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithThreeDiskSizeOfOneZero, workerWithTwoDisk, workerWithThreeDisk, workerWithNoDisk,
				}}},
				masterWithThreeDiskSizeOfOneZero,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("there is a worker with 3 disks, one too small",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithOneDisk, workerWithTwoDisk, workerWithThreeDisk, workerWithThreeDiskSizeOfOneZero,
				}}},
				workerWithThreeDiskSizeOfOneZero,
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{},
				},
			),

			table.Entry("there is a master with 2 small disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithThreeDiskSizeOfOneZero, workerWithTwoDisk, workerWithThreeDisk, workerWithNoDisk,
				}}},
				masterWithThreeDiskSizeOfOneZero,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("there is a worker with 2 small disks",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithOneDisk, workerWithTwoDisk, workerWithThreeDisk, workerWithThreeDiskSizeOfOneZero,
				}}},
				workerWithTwoSmallDisk,
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons: []string{fmt.Sprintf(
						"Insufficient resources to deploy ODF in %s mode. ODF requires a minimum of 3 hosts. Each host must have at least 1 additional disk of %d GB minimum and an installation disk.",
						strings.ToLower(string(standardMode)),
						operator.getMinDiskSizeGB(0),
					)},
				},
			),

			table.Entry("there is a master with disk of size zero",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithOneDisk, workerWithTwoDisk, workerWithThreeDisk, workerWithThreeDiskSizeOfOneZero,
				}}},
				masterWithThreeDiskSizeOfOneZero,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),

			table.Entry("there is a worker with disk of size zero",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithOneDisk, workerWithTwoDisk, workerWithThreeDisk, workerWithThreeDiskSizeOfOneZero,
				}}},
				workerWithThreeDiskSizeOfOneZero,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),
		)
	})

	Context("ValidateCluster", func() {
		table.DescribeTable("unknown mode scenario: validateCluster when", func(cluster *common.Cluster, expectedResult api.ValidationResult) {
			result, _ := operator.ValidateCluster(ctx, cluster)
			Expect(result).To(Equal(expectedResult))
		},
			table.Entry("there is a single master",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
				}}},
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"The cluster must either have no dedicated worker nodes or at least three. Add or remove hosts, or change their roles configurations to meet the requirement."},
				},
			),

			table.Entry("there are two masters and one worker",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, workerWithTwoDisk,
				}}},
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"The cluster must either have no dedicated worker nodes or at least three. Add or remove hosts, or change their roles configurations to meet the requirement."},
				},
			),

			table.Entry("there are 3 masters and 2 workers",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithLessDiskSize, workerWithTwoDisk, workerWithInvalidInventory,
				}}},
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"The cluster must either have no dedicated worker nodes or at least three. Add or remove hosts, or change their roles configurations to meet the requirement."},
				},
			),

			table.Entry("there are 4 masters and 2 workers",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithLessDiskSize, masterWithOneDisk, workerWithTwoDisk, workerWithInvalidInventory,
				}}},
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"The cluster must either have no dedicated worker nodes or at least three. Add or remove hosts, or change their roles configurations to meet the requirement."},
				},
			),

			table.Entry("missing inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithLessDiskSize, masterWithOneDisk, workerWithTwoDisk, masterWithNoInventory,
				}}},
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"The cluster must either have no dedicated worker nodes or at least three. Add or remove hosts, or change their roles configurations to meet the requirement."},
				},
			),

			table.Entry("invalid inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithNoDisk, masterWithLessDiskSize, masterWithOneDisk, workerWithTwoDisk, workerWithInvalidInventory,
				}}},
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"The cluster must either have no dedicated worker nodes or at least three. Add or remove hosts, or change their roles configurations to meet the requirement."},
				},
			),
		)

		table.DescribeTable("compact mode scenario: validateCluster when", func(cluster *common.Cluster, expectedResult api.ValidationResult) {
			result, _ := operator.ValidateCluster(ctx, cluster)
			Expect(result).To(Equal(expectedResult))
		},
			table.Entry("there are 2 masters and 1 auto-assign",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, autoAssignHost,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Compact Deployment are satisfied."},
				},
			),

			table.Entry("there are 3 masters - sufficient resources",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithThreeDisk, masterWithThreeDisk,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Compact Deployment are satisfied."},
				},
			),

			table.Entry("there are 4 masters - sufficient resources",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Compact Deployment are satisfied."},
				},
			),

			table.Entry("there are 5 masters - sufficient resources",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithThreeDisk,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Compact Deployment are satisfied."},
				},
			),

			// host's validation will fail
			table.Entry("there are enough masters - one of the masters doesn't have eligible disk",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithOneDisk,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Compact Deployment are satisfied."},
				},
			),

			table.Entry("there are enough masters - one of the masters doesn't have eligible disk",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithOneDisk,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Compact Deployment are satisfied."},
				},
			),

			table.Entry("there are enough masters - one of the masters is missing inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithNoInventory,
				}}},
				api.ValidationResult{
					Status:       api.Pending,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Missing Inventory in some of the hosts"},
				},
			),

			table.Entry("there are enough masters - one of the masters has invalid inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithThreeDisk, masterWithThreeDiskSizeOfOneZero, masterWithInvalidInventory,
				}}},
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Failed to parse the inventory of some of the hosts"},
				},
			),

			table.Entry("there are enough masters - not enough resources",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDiskSizeOfOneZero, masterWithThreeDiskSizeOfOneZero, masterWithNoDisk, masterWithNoDisk,
				}}},
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Insufficient resources to deploy ODF in compact mode. ODF requires a minimum of 3 hosts. Each host must have at least 1 additional disk of 25 GB minimum and an installation disk."},
				},
			),
		)

		table.DescribeTable("standard mode scenario: validateCluster when", func(cluster *common.Cluster, expectedResult api.ValidationResult) {
			result, _ := operator.ValidateCluster(ctx, cluster)
			Expect(result).To(Equal(expectedResult))
		},
			// Will fail host validation
			table.Entry("there are 3 masters, 3 workers and 1 auto-assign",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDiskSizeOfOneZero,
					masterWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
					autoAssignHost,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Standard Deployment are satisfied."},
				},
			),

			table.Entry("there are 3 masters and 3 workers - sufficient resources",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDisk,
					masterWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Standard Deployment are satisfied."},
				},
			),

			table.Entry("there are 4 masters and 4 workers - sufficient resources",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDisk,
					masterWithThreeDisk,
					masterWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Standard Deployment are satisfied."},
				},
			),

			table.Entry("there are enough masters - one of the masters doesn't have eligible disk",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDisk,
					masterWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithNoDisk,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Standard Deployment are satisfied."},
				},
			),

			// host's validation will fail
			table.Entry("there are enough workers - one of the workers doesn't have eligible disk",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDisk,
					masterWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithNoDisk,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Standard Deployment are satisfied."},
				},
			),

			table.Entry("there are enough masters - one of the masters is missing inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDisk,
					masterWithNoInventory,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Standard Deployment are satisfied."},
				},
			),

			table.Entry("there are enough workers - one of the workers is missing inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDisk,
					masterWithNoInventory,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithNoInventory,
				}}},
				api.ValidationResult{
					Status:       api.Pending,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Missing Inventory in some of the hosts"},
				},
			),

			table.Entry("there are enough masters - one of the masters has invalid inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDisk,
					masterWithInvalidInventory,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Standard Deployment are satisfied."},
				},
			),

			table.Entry("there are enough workers - one of the workers has invalid inventory",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDisk,
					masterWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithInvalidInventory,
				}}},
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Failed to parse the inventory of some of the hosts"},
				},
			),

			table.Entry("there are enough masters - not enough resources",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDisk,
					masterWithNoDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
				}}},
				api.ValidationResult{
					Status:       api.Success,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"ODF Requirements for Standard Deployment are satisfied."},
				},
			),

			table.Entry("there are enough workers - not enough resources",
				&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Hosts: []*models.Host{
					masterWithThreeDisk,
					masterWithThreeDisk,
					masterWithThreeDisk,
					workerWithThreeDisk,
					workerWithThreeDisk,
					workerWithNoDisk,
				}}},
				api.ValidationResult{
					Status:       api.Failure,
					ValidationId: operator.GetHostValidationID(),
					Reasons:      []string{"Insufficient resources to deploy ODF in standard mode. ODF requires a minimum of 3 hosts. Each host must have at least 1 additional disk of 25 GB minimum and an installation disk."},
				},
			),
		)
	})
})
