package common

import (
	"strings"

	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
)

// GetDisksToBeFormatted inspects a host's inventory and returns a list of all
// disks that will by default undergo formatting. This list even includes disks
// specified in the host's skip_formatting_disks list.
func GetDisksToBeFormatted(inventory *models.Inventory) []*models.Disk {
	formatDisks := make([]*models.Disk, 0, len(inventory.Disks))

	skipDriveTypes := []string{string(models.DriveTypeFC), string(models.DriveTypeISCSI), string(models.DriveTypeLVM), string(models.DriveTypeMultipath)}
	for _, disk := range inventory.Disks {
		diskRemovable := disk.Removable || strings.Contains(disk.ByPath, "mmcblk") //mmc devices should be treated as removable

		if disk.Bootable && !diskRemovable && !funk.Contains(skipDriveTypes, string(disk.DriveType)) && !disk.IsInstallationMedia {
			formatDisks = append(formatDisks, disk)
		}
	}

	return formatDisks
}

func GetDisksIdentifiersToBeFormatted(inventory *models.Inventory) []string {
	disksToBeFormatted := GetDisksToBeFormatted(inventory)

	disksToBeFormattedIdentifiers := make([]string, 0, len(disksToBeFormatted))
	for _, diskToBeFormatted := range disksToBeFormatted {
		disksToBeFormattedIdentifiers = append(disksToBeFormattedIdentifiers, GetDeviceIdentifier(diskToBeFormatted))
	}

	return disksToBeFormattedIdentifiers
}

func GetSkippedFormattingDiskIdentifiers(host *models.Host) []string {
	if host.SkipFormattingDisks != "" {
		return strings.Split(host.SkipFormattingDisks, ",")
	}

	return []string{}
}
