package common

import (
	"github.com/openshift/assisted-service/models"
)

const (
	VmwareManufacturer string = "VMware, Inc."
)

var supportedManufacturers = map[string]models.PlatformType{
	VmwareManufacturer: models.PlatformTypeVsphere,
}

func GetPlatformFromVendor(vendor models.SystemVendor) models.PlatformType {
	return supportedManufacturers[vendor.Manufacturer]
}

func GetHostSupportedPlatforms(inventory models.Inventory) *[]models.PlatformType {
	hostPlatform := []models.PlatformType{models.PlatformTypeBaremetal}

	if platform := GetPlatformFromVendor(*inventory.SystemVendor); platform != "" {
		hostPlatform = append(hostPlatform, platform)
	}

	return &hostPlatform
}

func GetClusterSupportedPlatforms(cluster Cluster, hostsPlatformsCounter map[models.PlatformType]int) *[]models.PlatformType {
	supportedPlatforms := make([]models.PlatformType, 0)
	hostsCount := len(cluster.Hosts)

	// SNO or no hosts
	if hostsCount == 0 || *cluster.HighAvailabilityMode != models.ClusterHighAvailabilityModeFull {
		supportedPlatforms = append(supportedPlatforms, models.PlatformTypeBaremetal)
		return &supportedPlatforms
	}

	for platform, count := range hostsPlatformsCounter {
		if count == hostsCount {
			supportedPlatforms = append(supportedPlatforms, platform)
		}
	}
	return &supportedPlatforms
}
