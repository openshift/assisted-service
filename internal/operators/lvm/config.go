package lvm

import (
	"github.com/openshift/assisted-service/models"
)

const (
	// LvmMinOpenshiftVersion is the minimum OCP version in which lvmo is supported
	LvmMinOpenshiftVersion string = "4.12.0-0.0"
)

type Config struct {
	LvmCPUPerHost          int64  `envconfig:"LVM_CPU_PER_HOST" default:"1"`
	LvmMemoryPerHostMiB    int64  `envconfig:"LVM_MEMORY_PER_HOST_MIB" default:"1200"`
	LvmMinDiskSizeGB       int64  `envconfig:"LVM_MIN_DISK_SIZE_GB" default:"0"`
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
