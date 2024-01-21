package lvm

import (
	"github.com/openshift/assisted-service/models"
)

const (
	// LvmMinOpenshiftVersion is the minimum OCP version in which lvmo is supported
	// Any changes here should be updated at line 16 too.
	LvmsMinOpenshiftVersion                            string = "4.11.0"
	LvmsMinOpenshiftVersion4_12                        string = "4.12.0"
	LvmsMinOpenshiftVersion_ForNewResourceRequirements string = "4.13.0"

	LvmoSubscriptionName string = "odf-lvm-operator"
	LvmsSubscriptionName string = "lvms-operator"

	// LvmsMemoryRequirement           int64 = 400
	// LvmsMemoryRequirementBefore4_13 int64 = 1200

)

type Config struct {
	LvmCPUPerHost                 int64  `envconfig:"LVM_CPU_PER_HOST" default:"1"`
	LvmMemoryPerHostMiB           int64  `envconfig:"LVM_MEMORY_PER_HOST_MIB" default:"400"`
	LvmMemoryPerHostMiBBefore4_13 int64  `envconfig:"LVM_MEMORY_PER_HOST_MIB" default:"1200"`
	LvmMinOpenshiftVersion        string `envconfig:"LVM_MIN_OPENSHIFT_VERSION" default:"4.11.0"`
	LvmMinMultiNodeSupportVersion string `envconfig:"LVM_MIN_MULTI_NODE_SUPPORT_VERSION" default:"4.15.0"`
}

// count all disks of drive type ssd or hdd
func (o *operator) getValidDiskCount(disks []*models.Disk, installationDiskID string) int64 {
	var countDisks int64

	for _, disk := range disks {
		if (disk.DriveType == models.DriveTypeSSD || disk.DriveType == models.DriveTypeHDD) && installationDiskID != disk.ID && disk.SizeBytes != 0 {
			countDisks++
		}
	}
	return countDisks
}
