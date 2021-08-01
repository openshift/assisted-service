package common

import (
	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
)

const (
	VmwareManufacturer string = "VMware, Inc."
)

var supportedManufacturers = map[string]models.PlatformType{
	VmwareManufacturer: models.PlatformTypeVsphere,
}

func GetAllSupportedPlatforms() *[]models.PlatformType {
	return &[]models.PlatformType{
		models.PlatformTypeVsphere,
		models.PlatformTypeBaremetal,
	}
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

func GetClusterSupportedPlatforms(cluster Cluster) (*[]models.PlatformType, error) {
	hostsCount := len(cluster.Hosts)

	// SNO or no hosts
	if hostsCount == 0 || *cluster.HighAvailabilityMode != models.ClusterHighAvailabilityModeFull {
		return &[]models.PlatformType{models.PlatformTypeBaremetal}, nil
	}

	hostsSupportedPlatforms := *GetAllSupportedPlatforms()
	for _, h := range cluster.Hosts {
		inventory, err := UnmarshalInventory(h.Inventory)
		if err != nil {
			return nil, err
		}
		hostsSupportedPlatforms = funk.Join(hostsSupportedPlatforms, *GetHostSupportedPlatforms(*inventory), funk.InnerJoin).([]models.PlatformType)
	}

	return &hostsSupportedPlatforms, nil
}
