package ocs

import (
	"context"
	"github.com/openshift/assisted-service/internal/operators/api"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

var _ = Describe("Ocs Operator", func() {
	var (
		ctx                      = context.TODO()
		operator                 = NewOcsOperator(common.GetTestLog(), nil)
		diskID1                  = "/dev/disk/by-id/test-disk-1"
		diskID2                  = "/dev/disk/by-id/test-disk-2"
		diskID3                  = "/dev/disk/by-id/test-disk-3"
		masterLabeledWithNoDisk  = &models.Host{Role: models.HostRoleMaster, Labels: ocsLabel, Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB})}
		masterLabeledWithOneDisk = &models.Host{Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1}}})}
		masterLabeledWithTwoDisk = &models.Host{Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
				}})}
		masterLabeledWithThreeDisk = &models.Host{Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID3},
				}})}
		masterUnlabeledWithThreeDisk = &models.Host{Role: models.HostRoleMaster, Labels: "", InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID3},
				}})}
		masterLabeledWithNoInventory  = &models.Host{Role: models.HostRoleMaster, Labels: ocsLabel}
		masterLabeledWithLessDiskSize = &models.Host{Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
					{SizeBytes: 20 * conversions.GB, DriveType: "SSD", ID: diskID2},
				}})}
		autoAssignLabeledHost = &models.Host{Role: models.HostRoleAutoAssign, Labels: ocsLabel, SuggestedRole: models.HostRoleAutoAssign, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
				}})}
		workerLabeledWithNoDisk  = &models.Host{Role: models.HostRoleWorker, Labels: ocsLabel, Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB})}
		workerLabeledWithTwoDisk = &models.Host{Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
				}})}
		workerLabeledWithThreeDisk = &models.Host{Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
					{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID3},
				}})}
		workerUnlabeledWithThreeDisk = &models.Host{Role: models.HostRoleWorker, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
					{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID3},
				}})}
		workerLabeledWithNoInventory = &models.Host{Role: models.HostRoleWorker, Labels: ocsLabel}
		//masterWithThreeDiskSizeOfOneZero = &models.Host{Role: models.HostRoleMaster, InstallationDiskID: diskID1,
		//	Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
		//		Disks: []*models.Disk{
		//			{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
		//			{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
		//			{SizeBytes: 0 * conversions.GB, DriveType: "SSD", ID: diskID3},
		//		}})}
		//workerWithOneDisk = &models.Host{Role: models.HostRoleWorker, InstallationDiskID: diskID1,
		//	Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB,
		//		Disks: []*models.Disk{
		//			{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
		//		}})}
		//workerWithThreeDiskSizeOfOneZero = &models.Host{Role: models.HostRoleWorker, InstallationDiskID: diskID1,
		//	Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB,
		//		Disks: []*models.Disk{
		//			{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
		//			{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
		//			{SizeBytes: 0 * conversions.GB, DriveType: "HDD", ID: diskID3},
		//		}})}
		workerLabeledWithLessDiskSize = &models.Host{Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB,
				Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: "SSD", ID: diskID2},
					{SizeBytes: 20 * conversions.GB, DriveType: "SSD", ID: diskID2},
				}})}
	)

	Context("GetHostRequirements", func() {
		table.DescribeTable("compact mode scenario: get requirements for hosts when ", func(cluster *common.Cluster, host *models.Host, expectedResult *models.ClusterHostRequirementsDetails) {
			res, _ := operator.GetHostRequirements(ctx, cluster, host)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("HostRequirements of a labeled master in a cluster with one host",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithThreeDisk,
				}}},
				masterLabeledWithThreeDisk,
				&models.ClusterHostRequirementsDetails{CPUCores: operator.config.OCSPerHostCPUCompactMode + 2*operator.config.OCSPerDiskCPUCount, RAMMib: conversions.GibToMib(operator.config.OCSPerHostMemoryGiBCompactMode + 2*operator.config.OCSPerDiskRAMGiB)},
			),
			table.Entry("HostRequirements of an unlabeled master in a cluster with one host",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterUnlabeledWithThreeDisk,
				}}},
				masterUnlabeledWithThreeDisk,
				&models.ClusterHostRequirementsDetails{CPUCores: 0, RAMMib: 0},
			),
			table.Entry("HostRequirements of a labeled master with no disk in a cluster with three hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithThreeDisk,
				}}},
				masterLabeledWithNoDisk,
				&models.ClusterHostRequirementsDetails{CPUCores: operator.config.OCSPerHostCPUCompactMode + operator.config.OCSPerDiskCPUCount, RAMMib: conversions.GibToMib(operator.config.OCSPerHostMemoryGiBCompactMode + operator.config.OCSPerDiskRAMGiB)},
			),
			table.Entry("HostRequirements of a labeled master with no inventory in a cluster with three hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithThreeDisk, masterLabeledWithNoInventory,
				}}},
				masterLabeledWithNoInventory,
				&models.ClusterHostRequirementsDetails{CPUCores: operator.config.OCSPerHostCPUCompactMode + operator.config.OCSPerDiskCPUCount, RAMMib: conversions.GibToMib(operator.config.OCSPerHostMemoryGiBCompactMode + operator.config.OCSPerDiskRAMGiB)},
			),
			table.Entry("HostRequirements of a labeled auto-assign host in a cluster with three hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithThreeDisk, autoAssignLabeledHost,
				}}},
				autoAssignLabeledHost,
				&models.ClusterHostRequirementsDetails{CPUCores: operator.config.OCSPerHostCPUCompactMode + operator.config.OCSPerDiskCPUCount, RAMMib: conversions.GibToMib(operator.config.OCSPerHostMemoryGiBCompactMode + operator.config.OCSPerDiskRAMGiB)},
			),
			table.Entry("HostRequirements of a labeled worker in a cluster with three hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithThreeDisk, workerLabeledWithThreeDisk,
				}}},
				workerLabeledWithThreeDisk,
				&models.ClusterHostRequirementsDetails{CPUCores: operator.config.OCSPerHostCPUStandardMode + 2*operator.config.OCSPerDiskCPUCount, RAMMib: conversions.GibToMib(operator.config.OCSPerHostMemoryGiBStandardMode + 2*operator.config.OCSPerDiskRAMGiB)},
			),
		)

		table.DescribeTable("standard mode scenario: get requirements for hosts when ", func(cluster *common.Cluster, host *models.Host, expectedResult *models.ClusterHostRequirementsDetails) {
			res, _ := operator.GetHostRequirements(ctx, cluster, host)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("HostRequirements of a labeled master in a cluster with six hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithTwoDisk, masterLabeledWithThreeDisk, workerLabeledWithNoDisk, workerLabeledWithThreeDisk,
				}}},
				masterLabeledWithThreeDisk,
				&models.ClusterHostRequirementsDetails{CPUCores: 0, RAMMib: 0},
			),
			table.Entry("HostRequirements of a labeled auto-assign host in a cluster with four hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithThreeDisk, autoAssignLabeledHost,
				}}},
				autoAssignLabeledHost,
				&models.ClusterHostRequirementsDetails{CPUCores: operator.config.OCSPerHostCPUStandardMode + operator.config.OCSPerDiskCPUCount, RAMMib: conversions.GibToMib(operator.config.OCSPerHostMemoryGiBStandardMode + operator.config.OCSPerDiskRAMGiB)},
			),
			table.Entry("HostRequirements of a labeled worker in a cluster with six hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithTwoDisk, masterLabeledWithThreeDisk, workerLabeledWithNoDisk, workerLabeledWithThreeDisk,
				}}},
				workerLabeledWithThreeDisk,
				&models.ClusterHostRequirementsDetails{CPUCores: operator.config.OCSPerHostCPUStandardMode + 2*operator.config.OCSPerDiskCPUCount, RAMMib: conversions.GibToMib(operator.config.OCSPerHostMemoryGiBStandardMode + 2*operator.config.OCSPerDiskRAMGiB)},
			),
			table.Entry("HostRequirements of a labeled worker with no disk in a cluster with six hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithThreeDisk, workerLabeledWithNoDisk, workerLabeledWithTwoDisk, workerLabeledWithThreeDisk,
				}}},
				workerLabeledWithNoDisk,
				&models.ClusterHostRequirementsDetails{CPUCores: operator.config.OCSPerHostCPUStandardMode, RAMMib: conversions.GibToMib(operator.config.OCSPerHostMemoryGiBStandardMode)},
			),
			table.Entry("HostRequirements of a labeled worker with no inventory in a cluster with six hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithThreeDisk, workerLabeledWithNoDisk, workerLabeledWithThreeDisk, workerLabeledWithNoInventory,
				}}},
				workerLabeledWithNoInventory,
				&models.ClusterHostRequirementsDetails{CPUCores: operator.config.OCSPerHostCPUStandardMode, RAMMib: conversions.GibToMib(operator.config.OCSPerHostMemoryGiBStandardMode)},
			),
			//	table.Entry("there are 6 hosts, worker with three disk requirements and Disk not Installation Eligible",
			//		&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
			//			masterWithThreeDisk, masterWithNoDisk, masterWithOneDisk, workerWithTwoDisk, workerWithThreeDiskSizeOfOneZero, workerWithNoDisk,
			//		}}},
			//		workerWithThreeDiskSizeOfOneZero,
			//		&models.ClusterHostRequirementsDetails{CPUCores: operator.config.OCSPerHostCPUStandardMode + operator.config.OCSPerDiskCPUCount, RAMMib: conversions.GibToMib(operator.config.OCSPerHostMemoryGiBStandardMode + operator.config.OCSPerDiskRAMGiB)},
			//	),
			//	table.Entry("there are 6 hosts, worker with two disk requirements",
			//		&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
			//			masterWithThreeDisk, masterWithNoDisk, masterWithOneDisk, workerWithTwoDisk, workerWithThreeDisk, workerWithNoDisk,
			//		}}},
			//		workerWithTwoDisk,
			//		&models.ClusterHostRequirementsDetails{CPUCores: operator.config.OCSPerHostCPUStandardMode + operator.config.OCSPerDiskCPUCount, RAMMib: conversions.GibToMib(operator.config.OCSPerHostMemoryGiBStandardMode + operator.config.OCSPerDiskRAMGiB)},
			//	),
			//	table.Entry("there are 6 hosts, worker with one disk requirements",
			//		&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
			//			masterWithThreeDisk, masterWithNoDisk, masterWithOneDisk, workerWithTwoDisk, workerWithThreeDisk, workerWithOneDisk,
			//		}}},
			//		workerWithOneDisk,
			//		&models.ClusterHostRequirementsDetails{CPUCores: operator.config.OCSPerHostCPUStandardMode, RAMMib: conversions.GibToMib(operator.config.OCSPerHostMemoryGiBStandardMode)},
			//	),
		)
	})

	Context("ValidateHost", func() {
		table.DescribeTable("compact mode scenario: validateHost when ", func(cluster *common.Cluster, host *models.Host, expectedResult api.ValidationResult) {
			res, _ := operator.ValidateHost(ctx, cluster, host)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("ValidateHost of a labeled master with three disks in a cluster with one host",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithThreeDisk,
				}}},
				masterLabeledWithThreeDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),
			table.Entry("ValidateHost of an unlabeled master with three disks in a cluster with one host",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterUnlabeledWithThreeDisk,
				}}},
				masterUnlabeledWithThreeDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{"Host not selected for OCS"}},
			),
			table.Entry("ValidateHost of a labeled master with no disk in a cluster with three hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithThreeDisk,
				}}},
				masterLabeledWithNoDisk,
				api.ValidationResult{Status: api.Failure, ValidationId: operator.GetHostValidationID(), Reasons: []string{"In compact mode, OCS requires at least one non-bootable disk on each labeled host"}},
			),
			table.Entry("ValidateHost of a labeled master with one disk in a cluster with three hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithThreeDisk,
				}}},
				masterLabeledWithOneDisk,
				api.ValidationResult{Status: api.Failure, ValidationId: operator.GetHostValidationID(), Reasons: []string{"In compact mode, OCS requires at least one non-bootable disk on each labeled host"}},
			),
			table.Entry("ValidateHost of a labeled auto-assign host with three disks in a cluster with three hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithThreeDisk, autoAssignLabeledHost,
				}}},
				autoAssignLabeledHost,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),
			table.Entry("ValidateHost of a labeled worker with three disks in a cluster with three hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithThreeDisk, workerLabeledWithThreeDisk,
				}}},
				workerLabeledWithThreeDisk,
				api.ValidationResult{Status: api.Failure, ValidationId: operator.GetHostValidationID(), Reasons: []string{"In compact mode, host role must be master or auto-assign"}},
			),
			table.Entry("ValidateHost of a labeled master with less disk size than minimum required in a cluster with three hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithThreeDisk, masterLabeledWithLessDiskSize,
				}}},
				masterLabeledWithLessDiskSize,
				api.ValidationResult{Status: api.Failure, ValidationId: operator.GetHostValidationID(), Reasons: []string{"OCS requires all the non-bootable disks to be more than 25 GB"}},
			),
		)
		table.DescribeTable("standard mode scenario: validateHosts when ", func(cluster *common.Cluster, host *models.Host, expectedResult api.ValidationResult) {
			res, _ := operator.ValidateHost(ctx, cluster, host)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("ValidateHost of a labeled worker with three disks in a cluster with six hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithThreeDisk, workerLabeledWithNoDisk, workerLabeledWithTwoDisk, workerLabeledWithThreeDisk,
				}}},
				workerLabeledWithThreeDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),
			table.Entry("ValidateHost of an unlabeled worker with three disks in a cluster with six hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithThreeDisk, workerLabeledWithNoDisk, workerLabeledWithTwoDisk, workerUnlabeledWithThreeDisk,
				}}},
				workerUnlabeledWithThreeDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{"Host not selected for OCS"}},
			),
			table.Entry("ValidateHost of a labeled worker with no disk in a cluster with six hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithThreeDisk, workerLabeledWithNoDisk, workerLabeledWithTwoDisk, workerLabeledWithThreeDisk,
				}}},
				workerLabeledWithNoDisk,
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID(), Reasons: []string{}},
			),
			table.Entry("ValidateHost of a labeled worker with no inventory in a cluster with six hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithThreeDisk, workerLabeledWithNoDisk, workerLabeledWithThreeDisk, workerLabeledWithNoInventory,
				}}},
				workerLabeledWithNoInventory,
				api.ValidationResult{Status: api.Pending, ValidationId: operator.GetHostValidationID(), Reasons: []string{"Missing Inventory in host"}},
			),
			table.Entry("ValidateHost of a labeled auto-assign host with three disks in a cluster with six hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithThreeDisk, autoAssignLabeledHost, workerLabeledWithNoDisk, workerLabeledWithThreeDisk,
				}}},
				autoAssignLabeledHost,
				api.ValidationResult{Status: api.Failure, ValidationId: operator.GetHostValidationID(), Reasons: []string{"In standard mode, host role must be master or worker"}},
			),
			table.Entry("ValidateHost of a labeled worker with less disk size than minimum required in a cluster with three hosts",
				&common.Cluster{Cluster: models.Cluster{Hosts: []*models.Host{
					masterLabeledWithNoDisk, masterLabeledWithOneDisk, masterLabeledWithThreeDisk, workerLabeledWithNoDisk, workerLabeledWithThreeDisk, workerLabeledWithLessDiskSize,
				}}},
				workerLabeledWithLessDiskSize,
				api.ValidationResult{Status: api.Failure, ValidationId: operator.GetHostValidationID(), Reasons: []string{"OCS requires all the non-bootable disks to be more than 25 GB"}},
			),
		)
	})

})
