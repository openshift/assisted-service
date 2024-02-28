package common

import (
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

// Returns count for disks that are not installion disk and fulfill size requirements (eligible disks) and
// disks that are not installation disk (available disks)
func NonInstallationDiskCount(disks []*models.Disk, installationDiskID string, minSizeGB int64) (int64, int64) {
	var eligibleDisks int64
	var availableDisks int64

	for _, disk := range disks {
		if (disk.DriveType == models.DriveTypeSSD || disk.DriveType == models.DriveTypeHDD) && installationDiskID != disk.ID && disk.SizeBytes != 0 {
			if disk.SizeBytes >= conversions.GbToBytes(minSizeGB) {
				eligibleDisks++
			} else {
				availableDisks++
			}
		}
	}
	return eligibleDisks, availableDisks
}

func HasOperator(operators []*models.MonitoredOperator, operatorName string) bool {
	for _, o := range operators {
		if o.Name == operatorName {
			return true
		}
	}
	return false
}
