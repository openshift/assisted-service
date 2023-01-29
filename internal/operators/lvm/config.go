package lvm

import (
	"github.com/openshift/assisted-service/models"
)

const (
	// LvmMinOpenshiftVersion is the minimum OCP version in which lvmo is supported
	// Any changes here should be updated at line 16 too.
	LvmoMinOpenshiftVersion string = "4.11.0"
	LvmsMinOpenshiftVersion string = "4.12.0"

	LvmoSubscriptionName string = "odf-lvm-operator"
	LvmsSubscriptionName string = "lvms-operator"
)

type Config struct {
	LvmCPUPerHost          int64  `envconfig:"LVM_CPU_PER_HOST" default:"1"`
	LvmMemoryPerHostMiB    int64  `envconfig:"LVM_MEMORY_PER_HOST_MIB" default:"1200"`
	LvmMinOpenshiftVersion string `envconfig:"LVM_MIN_OPENSHIFT_VERSION" default:"4.11.0"`
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
