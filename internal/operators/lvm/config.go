package lvm

import (
	"fmt"

	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

type Config struct {
	LvmCPUPerHost          int64  `envconfig:"LVM_CPU_PER_HOST" default:"1"`
	LvmMemoryPerHostMiB    int64  `envconfig:"LVM_MEMORY_PER_HOST_MIB" default:"1200"`
	LvmMinDiskSizeGB       int64  `envconfig:"LVM_MIN_DISK_SIZE_GB" default:"0"`
	LvmMinOpenshiftVersion string `envconfig:"LVM_MIN_OPENSHIFT_VERSION" default:"4.11.0"`
}

// count all disks of drive type ssd or hdd
func (o *operator) getValidDiskCount(disks []*models.Disk, installationDiskID string) (int64, error) {
	var countDisks int64
	var err error

	for _, disk := range disks {
		if (disk.DriveType == models.DriveTypeSSD || disk.DriveType == models.DriveTypeHDD) && installationDiskID != disk.ID && disk.SizeBytes != 0 {
			if disk.SizeBytes < conversions.GbToBytes(o.config.LvmMinDiskSizeGB) {
				err = fmt.Errorf("ODF LVM requires all the non-bootable disks to be more than %d GB", o.config.LvmMinDiskSizeGB)
			} else {
				countDisks++
			}
		}
	}
	return countDisks, err
}
